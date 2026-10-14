package sqln

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	sqln_driver "github.com/joaoprofile/gofi-sdk-go/sqln/driver"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

var productIndex = fts.MustIndex("products", fts.Spec{Name: "ftx_products", Columns: []fts.Column{{Name: "name"}}})

// ftsDialect is the fake dialect with PostgreSQL's full-text rendering.
type ftsDialect struct {
	fakeDialect
	sqln_driver.FullTextDialect
}

var pgFTS = ftsDialect{FullTextDialect: postgres.PostgresDialect{}}

const ftsDriverName = "sqln-fts-testdriver"

type ftsConnDriver struct{ fakeConnDriver }

func (ftsConnDriver) Name() connection.DriverName  { return ftsDriverName }
func (ftsConnDriver) Dialect() sqln_driver.Dialect { return pgFTS }

var registerFTS sync.Once

// ftsConn reaches the fake database whose queries all return no rows, so the
// index lookup reports a missing index.
func ftsConn(t *testing.T, opts ...connection.Option) *connection.Connection {
	registerFTS.Do(func() { connection.RegisterDriver(ftsConnDriver{}) })
	return connection.NewRaw(openDB(t, "no-rows"), ftsConnDriver{}, opts...)
}

func searchProducts() *criteria.Query {
	return criteria.From("products", "p").Where(criteria.Match(productIndex.Search("cafe")))
}

func ranSearch(t *testing.T) bool {
	for _, q := range recorded(t) {
		if strings.Contains(q, "@@") {
			return true
		}
	}
	return false
}

func TestFullText_CriteriaRequiresTheIndex(t *testing.T) {
	resetRecordedQueries(t)
	_, err := FindFromCriteria[mappedItem](context.Background(), searchProducts()).WithConnection(ftsConn(t)).List()
	assert.ErrorIs(t, err, fts.ErrIndexMissing)
	assert.False(t, ranSearch(t), "the search must not run without its index")
	assert.Contains(t, strings.Join(recorded(t), "\n"), "obj_description", "the catalog was checked")
}

func TestFullText_PagedListRequiresTheIndex(t *testing.T) {
	_, err := FindFromCriteria[mappedItem](context.Background(), searchProducts()).
		WithConnection(ftsConn(t)).WithPage(NewPageRequest(0, 10, nil)).PagedList()
	assert.ErrorIs(t, err, fts.ErrIndexMissing)
}

func TestFullText_TrustSkipsTheCheck(t *testing.T) {
	resetRecordedQueries(t)
	_, err := FindFromCriteria[mappedItem](context.Background(), searchProducts()).
		WithConnection(ftsConn(t, connection.WithFullTextPolicy(fts.Trust))).List()
	require.NoError(t, err)
	assert.True(t, ranSearch(t))
	assert.NotContains(t, strings.Join(recorded(t), "\n"), "obj_description")
}

func TestFullText_FilterAndRawQueriesRequireTheIndex(t *testing.T) {
	conn := ftsConn(t)
	qp, err := BuildQuery("SELECT * FROM products p WHERE p.active", nil,
		NewFilters().Add(NewFilter("q", Search, "cafe")),
		FilterMapping{"q": {Search: productIndex, SearchOn: "p"}}, pgFTS)
	require.NoError(t, err)
	assert.Equal(t, []*fts.Index{productIndex}, qp.Indexes)
	_, err = FindWithFilter[mappedItem](context.Background(), qp).WithConnection(conn).List()
	assert.ErrorIs(t, err, fts.ErrIndexMissing)

	_, err = Find[mappedItem](context.Background(), "SELECT 1").WithConnection(conn).RequireTextIndex(productIndex).List()
	assert.ErrorIs(t, err, fts.ErrIndexMissing)

	_, err = Find[mappedItem](context.Background(), "SELECT 1").WithConnection(conn).List()
	assert.NoError(t, err, "queries without a search are not checked")
}

func TestFullText_ConnectionChecksIndexesOnOpen(t *testing.T) {
	registerFTS.Do(func() { connection.RegisterDriver(ftsConnDriver{}) })
	initDriver()
	_, err := connection.NewConnection(connection.Config{
		Driver:   ftsDriverName,
		DSN:      "no-rows",
		FullText: connection.FullTextConfig{Indexes: []*fts.Index{productIndex}},
	})
	assert.ErrorIs(t, err, fts.ErrIndexMissing)

	conn, err := connection.NewConnection(connection.Config{
		Driver:   ftsDriverName,
		DSN:      "no-rows",
		FullText: connection.FullTextConfig{Policy: fts.Trust, Indexes: []*fts.Index{productIndex}},
	})
	require.NoError(t, err)
	_ = conn.Close()
}

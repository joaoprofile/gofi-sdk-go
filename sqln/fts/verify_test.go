package fts_test

import (
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/oracle"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

// catalog is a fake database answering the index lookup with comment (no
// row when empty) and the token size check with tokenSize. A statement
// carrying COMMENT stores its comment, as creating the index would.
type catalog struct {
	mu        sync.Mutex
	comment   string
	tokenSize string
	// MySQL: the checked collation and the column count of a weight index.
	collation, groupColumns string
	execs                   []string
	lookups                 atomic.Int32
}

var (
	catalogs   sync.Map // DSN → *catalog
	registered sync.Once
	nextDSN    atomic.Int64
)

func openCatalog(t *testing.T, c *catalog) *sql.DB {
	registered.Do(func() { sql.Register("fts-catalog", catalogDriver{}) })
	dsn := t.Name() + string(rune('a'+nextDSN.Add(1)))
	catalogs.Store(dsn, c)
	db, err := sql.Open("fts-catalog", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

type catalogDriver struct{}

func (catalogDriver) Open(dsn string) (driver.Conn, error) {
	c, _ := catalogs.Load(dsn)
	return catalogConn{c.(*catalog)}, nil
}

type catalogConn struct{ c *catalog }

func (cc catalogConn) Prepare(q string) (driver.Stmt, error) { return catalogStmt{cc.c, q}, nil }
func (catalogConn) Close() error                             { return nil }
func (catalogConn) Begin() (driver.Tx, error)                { return nil, driver.ErrSkip }

type catalogStmt struct {
	c *catalog
	q string
}

func (catalogStmt) Close() error  { return nil }
func (catalogStmt) NumInput() int { return -1 }

func (s catalogStmt) Exec([]driver.Value) (driver.Result, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.execs = append(s.c.execs, s.q)
	if _, after, ok := strings.Cut(s.q, "'gofi-fts:"); ok {
		s.c.comment = "gofi-fts:" + strings.TrimSuffix(after, "'")
	}
	return driver.RowsAffected(0), nil
}

func (s catalogStmt) Query([]driver.Value) (driver.Rows, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	switch {
	case strings.Contains(s.q, "innodb_ft_min_token_size"):
		return &catalogRows{values: []string{s.c.tokenSize}}, nil
	case strings.Contains(s.q, "COLLATION_NAME"):
		return &catalogRows{values: []string{cmp.Or(s.c.collation, "accent-insensitive")}}, nil
	case strings.Contains(s.q, "COUNT(*)"):
		return &catalogRows{values: []string{cmp.Or(s.c.groupColumns, "1")}}, nil
	}
	s.c.lookups.Add(1)
	if s.c.comment == "" {
		return &catalogRows{}, nil
	}
	return &catalogRows{values: []string{s.c.comment}}, nil
}

type catalogRows struct{ values []string }

func (*catalogRows) Columns() []string { return []string{"v"} }
func (*catalogRows) Close() error      { return nil }
func (r *catalogRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	dest[0], r.values = r.values[0], r.values[1:]
	return nil
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	pg := postgres.PostgresDialect{}
	want, _ := products.Comment(pg)

	err := fts.Check(ctx, openCatalog(t, &catalog{}), pg, products)
	assert.ErrorIs(t, err, fts.ErrIndexMissing)

	err = fts.Check(ctx, openCatalog(t, &catalog{comment: "gofi-fts:v1:0000000000000000"}), pg, products)
	assert.ErrorIs(t, err, fts.ErrIndexOutdated)

	err = fts.Check(ctx, openCatalog(t, &catalog{comment: "created by hand"}), pg, products)
	assert.ErrorIs(t, err, fts.ErrIndexOutdated, "an index sqln did not create is not trusted")

	assert.NoError(t, fts.Check(ctx, openCatalog(t, &catalog{comment: want}), pg, products))

	assert.ErrorIs(t, fts.Check(ctx, openCatalog(t, &catalog{}), oracle.OracleDialect{}, products), fts.ErrUnsupported)
}

func TestCheckMySQLTokenSize(t *testing.T) {
	ctx := context.Background()
	my := mysql.MySQLDialect{}
	want, _ := products.Comment(my)

	assert.NoError(t, fts.Check(ctx, openCatalog(t, &catalog{comment: want, tokenSize: "3"}), my, products))
	err := fts.Check(ctx, openCatalog(t, &catalog{comment: want, tokenSize: "2"}), my, products)
	assert.ErrorIs(t, err, fts.ErrServerConfig)
	assert.ErrorContains(t, err, "innodb_ft_min_token_size is 2")

	err = fts.Check(ctx, openCatalog(t, &catalog{comment: want, tokenSize: "3", collation: "utf8mb4_0900_as_cs"}), my, products)
	assert.ErrorIs(t, err, fts.ErrServerConfig, "Unaccent needs an accent-insensitive collation")
	assert.ErrorContains(t, err, "collation of name is utf8mb4_0900_as_cs")

	err = fts.Check(ctx, openCatalog(t, &catalog{comment: want, tokenSize: "3", groupColumns: "0"}), my, products)
	assert.ErrorIs(t, err, fts.ErrIndexMissing, "a missing weight index is a missing index")
	assert.ErrorContains(t, err, "ftx_products_search_wa")
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	my := mysql.MySQLDialect{}

	c := &catalog{tokenSize: "3"}
	require.NoError(t, fts.Create(ctx, openCatalog(t, c), my, products))
	require.Len(t, c.execs, 3, "the index and its two weight group indexes")
	assert.Contains(t, c.execs[0], "ADD FULLTEXT INDEX ftx_products_search (")

	// Existing and current: nothing to run.
	c.execs = nil
	require.NoError(t, fts.Create(ctx, openCatalog(t, c), my, products))
	assert.Empty(t, c.execs)

	// Outdated: reported, never dropped.
	stale := &catalog{comment: "gofi-fts:v1:0000000000000000", tokenSize: "3"}
	assert.ErrorIs(t, fts.Create(ctx, openCatalog(t, stale), my, products), fts.ErrIndexOutdated)
	assert.Empty(t, stale.execs)
}

func TestApply(t *testing.T) {
	ctx := context.Background()
	pg := postgres.PostgresDialect{}

	c := &catalog{}
	assert.NoError(t, fts.Apply(ctx, fts.Trust, openCatalog(t, c), pg, products))
	assert.Zero(t, c.lookups.Load(), "Trust never reads the catalog")

	assert.ErrorIs(t, fts.Apply(ctx, fts.Verify, openCatalog(t, c), pg, products), fts.ErrIndexMissing)
	assert.Empty(t, c.execs, "Verify never creates")

	require.NoError(t, fts.Apply(ctx, fts.Ensure, openCatalog(t, c), pg, products))
	assert.Len(t, c.execs, 5)
}

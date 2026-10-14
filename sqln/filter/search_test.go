package filter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

var productSearch = fts.MustIndex("products", fts.Spec{
	Name: "ftx_products", Columns: []fts.Column{{Name: "name"}}, Language: fts.Portuguese,
})

var catalog = Mapping{
	"q":    {Search: productSearch, SearchOn: "p", Ops: []string{Search}},
	"auto": {Search: productSearch, SearchOn: "p", SearchMode: fts.Prefix},
	"name": {Column: "p.name", Ops: Text},
}

func TestBuild_Search(t *testing.T) {
	fs := NewFilters().Add(NewFilter("q", Search, `cafe -"de vidro"`), AND(), NewFilter("auto", Search, "exp"))
	qp, err := Build("SELECT * FROM products p WHERE p.tenant_id = $1", []any{"t-1"}, fs, catalog, postgres.PostgresDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM products p WHERE p.tenant_id = $1 AND ( "+
		"p.ftx_products_tsv @@ to_tsquery('portuguese'::regconfig, $2) AND "+
		"p.ftx_products_tsv @@ to_tsquery('portuguese'::regconfig, $3) )", qp.Query)
	assert.Equal(t, []any{"t-1", "'cafe' & !('de' <-> 'vidro')", "'exp':*"}, qp.Params)
	assert.Equal(t, []*fts.Index{productSearch}, qp.Indexes)
}

func TestBuild_SearchRejects(t *testing.T) {
	pg := postgres.PostgresDialect{}
	build := func(f *Filter) error {
		_, err := Build("SELECT * FROM products p WHERE 1=1", nil, NewFilters().Add(f), catalog, pg)
		return err
	}
	// SEARCH only exists on a field mapped to an index.
	err := build(NewFilter("name", Search, "cafe"))
	assert.ErrorIs(t, err, ErrInvalidFilter)
	assert.ErrorContains(t, err, `condition "SEARCH" is not allowed`)
	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("name", Search, "cafe")), Allow("name"), pg)
	assert.ErrorContains(t, err, "has no full-text index")

	// A search-only field has no column for other conditions.
	assert.ErrorContains(t, build(NewFilter("q", Contains, "cafe")), `condition "LIKE" is not allowed`)
	assert.ErrorContains(t, build(NewFilter("auto", Eq, "cafe")), "invalid column")

	assert.ErrorContains(t, build(NewFilter("q", Search, 42)), "requires a string")
	assert.ErrorContains(t, build(NewFilter("q", Search, strings.Repeat("a", DefaultMaxLikeLength+1))), "exceeds")

	err = build(NewFilter("q", Search, "!!!"))
	assert.ErrorIs(t, err, ErrInvalidFilter)
	assert.ErrorIs(t, err, fts.ErrEmptySearch, "callers can tell an empty search from a bad request")
}

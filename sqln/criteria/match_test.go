package criteria_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/oracle"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

var productSearch = fts.MustIndex("products", fts.Spec{
	Name:     "ftx_products_search",
	Columns:  []fts.Column{{Name: "name", Weight: fts.A}, {Name: "description", Weight: fts.B}},
	Language: fts.Portuguese,
	Unaccent: true,
})

var categorySearch = fts.MustIndex("categories", fts.Spec{
	Name:     "ftx_categories",
	Columns:  []fts.Column{{Name: "title"}},
	Strategy: fts.ExpressionIndex,
})

const pgQuery = "to_tsquery('gofi_portuguese_unaccent'::regconfig, "

func TestMatch_Postgres(t *testing.T) {
	s := productSearch.Search("notebook gamer")
	sql, params, err := criteria.From("products", "p").
		Select("p.id").
		SelectRelevance(s, "relevance").
		Where(criteria.IsTrue("p.active"), criteria.Match(s), criteria.Lte("p.price", 100)).
		OrderBy(criteria.ByRelevance(s), criteria.Asc("p.id")).
		Build(postgres.PostgresDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT p.id, ts_rank_cd(p.ftx_products_search_tsv, "+pgQuery+"$1)) AS relevance"+
		" FROM products p"+
		" WHERE p.active IS TRUE AND p.ftx_products_search_tsv @@ "+pgQuery+"$2) AND p.price <= $3"+
		" ORDER BY ts_rank_cd(p.ftx_products_search_tsv, "+pgQuery+"$4)) DESC, p.id ASC", sql)
	assert.Equal(t, []any{"'notebook' & 'gamer'", "'notebook' & 'gamer'", 100, "'notebook' & 'gamer'"}, params)
}

func TestMatch_PostgresExpressionIndexOnJoin(t *testing.T) {
	sql, _, err := criteria.From("products", "p").
		Join("categories", "c", "c.id = p.category_id").
		Where(criteria.Match(categorySearch.Search("livros").Mode(fts.Prefix))).
		Build(postgres.PostgresDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM products p JOIN categories c ON c.id = p.category_id"+
		" WHERE (to_tsvector('simple'::regconfig, coalesce(c.title, ''))) @@ to_tsquery('simple'::regconfig, $1)", sql)
}

func TestMatch_MySQL(t *testing.T) {
	s := productSearch.Search(`cafe -"de vidro"`)
	sql, params, err := criteria.From("products", "").
		SelectRelevance(s, "score").
		Where(criteria.Match(s)).
		Build(mysql.MySQLDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT *, (MATCH(products.name) AGAINST (? IN BOOLEAN MODE) * 1.0"+
		" + MATCH(products.description) AGAINST (? IN BOOLEAN MODE) * 0.4) AS score"+
		" FROM products WHERE MATCH(products.name, products.description) AGAINST (? IN BOOLEAN MODE)", sql)
	// The weight groups rank the positive terms without operators: a term
	// required by the search may sit in another group.
	assert.Equal(t, []any{"cafe", "cafe", `+cafe -"de vidro"`}, params)
}

func TestMatch_MySQLUnweightedRank(t *testing.T) {
	s := categorySearch.Search("livros infantis")
	sql, params, err := criteria.From("categories", "c").SelectRelevance(s, "score").Where(criteria.Match(s)).Build(mysql.MySQLDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT *, MATCH(c.title) AGAINST (? IN BOOLEAN MODE) AS score FROM categories c WHERE MATCH(c.title) AGAINST (? IN BOOLEAN MODE)", sql)
	assert.Equal(t, []any{"+livros +infantis", "+livros +infantis"}, params)
}

func TestMatch_InsideGroupAndBuildBase(t *testing.T) {
	q := criteria.From("products", "p").Where(criteria.Group(
		criteria.Match(productSearch.Search("cafe")), criteria.Or(), criteria.Eq("p.sku", "cafe"),
	))
	sql, _, err := q.BuildBase(mysql.MySQLDialect{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM products p WHERE (MATCH(p.name, p.description) AGAINST (? IN BOOLEAN MODE) OR p.sku = ?)", sql)
	assert.Equal(t, []*fts.Index{productSearch}, q.TextIndexes())
}

func TestMatch_Qualifier(t *testing.T) {
	// Self-join: ambiguous without On.
	q := func(s fts.Search) *criteria.Query {
		return criteria.From("products", "p").Join("products", "parent", "parent.id = p.parent_id").Where(criteria.Match(s))
	}
	_, _, err := q(productSearch.Search("cafe")).Build(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
	assert.ErrorContains(t, err, "appears 2 times")

	sql, _, err := q(productSearch.Search("cafe").On("parent")).Build(postgres.PostgresDialect{})
	require.NoError(t, err)
	assert.Contains(t, sql, "parent.ftx_products_search_tsv @@")

	// Table not in the query.
	_, _, err = criteria.From("orders", "o").Where(criteria.Match(productSearch.Search("cafe"))).Build(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, criteria.ErrInvalidField)

	// On is written into the SQL: it must be an alias.
	_, _, err = criteria.From("products", "p").Where(criteria.Match(productSearch.Search("cafe").On("p; DROP"))).Build(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, criteria.ErrInvalidField)

	// A bare clause has no tables: unqualified unless On is set.
	clause, params, err := criteria.BuildClauseAfter([]any{7}, []criteria.Predicate{criteria.Match(productSearch.Search("cafe"))}, postgres.PostgresDialect{})
	require.NoError(t, err)
	assert.Equal(t, "ftx_products_search_tsv @@ "+pgQuery+"$2)", clause)
	assert.Equal(t, []any{7, "'cafe'"}, params)
}

func TestMatch_Errors(t *testing.T) {
	build := func(s fts.Search, d interface {
		Param(int) string
		Like(string, string) string
		NotLike(string, string) string
	}) error {
		_, _, err := criteria.From("products", "p").Where(criteria.Match(s)).Build(d)
		return err
	}
	err := build(productSearch.Search("!!!"), postgres.PostgresDialect{})
	assert.ErrorIs(t, err, criteria.ErrInvalidValue)
	assert.ErrorIs(t, err, fts.ErrEmptySearch)

	assert.ErrorIs(t, build(productSearch.Search("tv de"), mysql.MySQLDialect{}), fts.ErrEmptySearch)
	assert.ErrorIs(t, build(productSearch.Search("cafe"), oracle.OracleDialect{}), fts.ErrUnsupported)

	_, _, err = criteria.From("products", "p").SelectRelevance(productSearch.Search("cafe"), "x; DROP").Build(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
}

func TestTextIndexes(t *testing.T) {
	q := criteria.From("products", "p").
		Join("categories", "c", "c.id = p.category_id").
		SelectRelevance(productSearch.Search("a"), "r").
		Where(criteria.Match(productSearch.Search("a")), criteria.Group(criteria.Match(categorySearch.Search("b")))).
		OrderBy(criteria.ByRelevance(productSearch.Search("a")))
	assert.Equal(t, []*fts.Index{productSearch, categorySearch}, q.TextIndexes())
	assert.Empty(t, criteria.From("products", "p").Where(criteria.Eq("p.id", 1)).TextIndexes())
}

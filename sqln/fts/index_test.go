package fts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/oracle"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

func TestNewIndexRejects(t *testing.T) {
	col := []fts.Column{{Name: "name"}}
	tests := map[string]struct {
		table string
		spec  fts.Spec
	}{
		"quoted table":       {`"products"`, fts.Spec{Name: "ftx", Columns: col}},
		"injected table":     {"products; DROP", fts.Spec{Name: "ftx", Columns: col}},
		"upper-case name":    {"products", fts.Spec{Name: "Ftx", Columns: col}},
		"long name":          {"products", fts.Spec{Name: "f23456789012345678901234567890123456789012345678901234567890", Columns: col}},
		"no columns":         {"products", fts.Spec{Name: "ftx"}},
		"expression column":  {"products", fts.Spec{Name: "ftx", Columns: []fts.Column{{Name: "lower(name)"}}}},
		"repeated column":    {"products", fts.Spec{Name: "ftx", Columns: []fts.Column{{Name: "name"}, {Name: "NAME"}}}},
		"bad weight":         {"products", fts.Spec{Name: "ftx", Columns: []fts.Column{{Name: "name", Weight: 'E'}}}},
		"unknown language":   {"products", fts.Spec{Name: "ftx", Columns: col, Language: "klingon"}},
		"injected language":  {"products", fts.Spec{Name: "ftx", Columns: col, Language: "simple'::regconfig, x) --"}},
		"vector is a column": {"products", fts.Spec{Name: "ftx", Columns: col, VectorColumn: "name"}},
		"bad strategy":       {"products", fts.Spec{Name: "ftx", Columns: col, Strategy: 9}},
		"bad token size":     {"products", fts.Spec{Name: "ftx", Columns: col, MinTokenSize: 99}},
	}
	for name, tt := range tests {
		_, err := fts.NewIndex(tt.table, tt.spec)
		assert.ErrorIs(t, err, fts.ErrInvalidIndex, name)
	}
	assert.Panics(t, func() { fts.MustIndex("products", fts.Spec{}) })
}

func TestIndexDefaults(t *testing.T) {
	idx := fts.MustIndex("shop.products", fts.Spec{Name: "ftx_products", Columns: []fts.Column{{Name: "name"}}})
	def := idx.Definition()
	assert.Equal(t, "simple", def.Language)
	assert.Equal(t, "ftx_products_tsv", def.VectorColumn)
	assert.Equal(t, fts.DefaultMinTokenSize, def.MinTokenSize)
	assert.Equal(t, fts.StoredVector, def.Strategy)
	def.Columns[0].Name = "changed"
	assert.Equal(t, "name", idx.Definition().Columns[0].Name, "Definition returns a copy")
}

func TestStatementsPostgres(t *testing.T) {
	up, down, err := fts.Statements(postgres.PostgresDialect{}, products)
	require.NoError(t, err)
	comment, err := products.Comment(postgres.PostgresDialect{})
	require.NoError(t, err)
	doc := "setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(name, '')), 'A') || " +
		"setweight(to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(description, '')), 'B')"
	assert.Equal(t, []string{
		"CREATE EXTENSION IF NOT EXISTS unaccent",
		"DO $$\nBEGIN\n" +
			"  IF NOT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'gofi_portuguese_unaccent') THEN\n" +
			"    CREATE TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent (COPY = portuguese);\n" +
			"    ALTER TEXT SEARCH CONFIGURATION gofi_portuguese_unaccent ALTER MAPPING FOR hword, hword_part, word WITH unaccent, portuguese_stem;\n" +
			"  END IF;\nEND $$",
		"ALTER TABLE products ADD COLUMN ftx_products_search_tsv tsvector GENERATED ALWAYS AS (" + doc + ") STORED",
		"CREATE INDEX ftx_products_search ON products USING GIN (ftx_products_search_tsv)",
		"COMMENT ON INDEX ftx_products_search IS '" + comment + "'",
	}, up)
	assert.Equal(t, []string{
		"DROP INDEX IF EXISTS ftx_products_search",
		"ALTER TABLE products DROP COLUMN IF EXISTS ftx_products_search_tsv",
	}, down)
}

func TestStatementsPostgresExpression(t *testing.T) {
	idx := fts.MustIndex("shop.items", fts.Spec{
		Name: "ftx_items", Columns: []fts.Column{{Name: "title"}}, Strategy: fts.ExpressionIndex,
	})
	up, down, err := fts.Statements(postgres.PostgresDialect{}, idx)
	require.NoError(t, err)
	require.Len(t, up, 2)
	assert.Equal(t, "CREATE INDEX ftx_items ON shop.items USING GIN ((to_tsvector('simple'::regconfig, coalesce(title, ''))))", up[0])
	assert.Contains(t, up[1], "COMMENT ON INDEX shop.ftx_items IS 'gofi-fts:v1:")
	assert.Equal(t, []string{"DROP INDEX IF EXISTS shop.ftx_items"}, down)
}

func TestStatementsMySQL(t *testing.T) {
	up, down, err := fts.Statements(mysql.MySQLDialect{}, products)
	require.NoError(t, err)
	comment, _ := products.Comment(mysql.MySQLDialect{})
	assert.Equal(t, []string{"ALTER TABLE products ADD FULLTEXT INDEX ftx_products_search (name, description) COMMENT '" + comment + "'"}, up)
	assert.Equal(t, []string{"ALTER TABLE products DROP INDEX ftx_products_search"}, down)

	_, _, err = fts.Statements(oracle.OracleDialect{}, products)
	assert.ErrorIs(t, err, fts.ErrUnsupported)
}

func TestCommentTracksTheDefinition(t *testing.T) {
	spec := fts.Spec{Name: "ftx_p", Columns: []fts.Column{{Name: "name", Weight: fts.A}}, Language: fts.Portuguese}
	base := fts.MustIndex("products", spec)
	pg := postgres.PostgresDialect{}
	my := mysql.MySQLDialect{}
	c1, _ := base.Comment(pg)
	c2, _ := fts.MustIndex("products", spec).Comment(pg)
	assert.Equal(t, c1, c2, "deterministic")
	assert.Regexp(t, `^gofi-fts:v1:[0-9a-f]{16}$`, c1)

	reweighted := spec
	reweighted.Columns = []fts.Column{{Name: "name", Weight: fts.B}}
	c3, _ := fts.MustIndex("products", reweighted).Comment(pg)
	assert.NotEqual(t, c1, c3, "a weight changes the PostgreSQL document")

	m1, _ := base.Comment(my)
	m3, _ := fts.MustIndex("products", reweighted).Comment(my)
	assert.Equal(t, m1, m3, "MySQL ignores weights: no migration needed")
}

func TestWriteMigration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, fts.WriteMigration(dir, 20261014, "products_search", mysql.MySQLDialect{}, products))
	up, err := os.ReadFile(filepath.Join(dir, "20261014_products_search.up.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(up), "ADD FULLTEXT INDEX ftx_products_search")
	down, err := os.ReadFile(filepath.Join(dir, "20261014_products_search.down.sql"))
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE products DROP INDEX ftx_products_search;\n", string(down))

	assert.Error(t, fts.WriteMigration(dir, 1, "../escape", mysql.MySQLDialect{}, products))
}

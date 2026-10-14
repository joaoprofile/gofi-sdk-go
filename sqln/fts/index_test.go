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
	assert.Equal(t, []string{
		"ALTER TABLE products ADD FULLTEXT INDEX ftx_products_search (name, description) COMMENT '" + comment + "'",
		"ALTER TABLE products ADD FULLTEXT INDEX ftx_products_search_wa (name)",
		"ALTER TABLE products ADD FULLTEXT INDEX ftx_products_search_wb (description)",
	}, up, "one index per weight group, for the ranking")
	assert.Equal(t, []string{
		"ALTER TABLE products DROP INDEX ftx_products_search_wa",
		"ALTER TABLE products DROP INDEX ftx_products_search_wb",
		"ALTER TABLE products DROP INDEX ftx_products_search",
	}, down)

	// Same weight everywhere (or none): no group index.
	plain := mustNew(t, "products", fts.Spec{Name: "ftx_plain", Columns: []fts.Column{{Name: "name", Weight: fts.B}, {Name: "sku", Weight: fts.B}}})
	up, _, err = fts.Statements(mysql.MySQLDialect{}, plain)
	require.NoError(t, err)
	assert.Len(t, up, 1)

	// Unweighted columns rank as D, below a weighted one.
	mixed := mustNew(t, "products", fts.Spec{Name: "ftx_mixed", Columns: []fts.Column{{Name: "name", Weight: fts.A}, {Name: "sku"}, {Name: "notes", Weight: fts.D}}})
	up, _, err = fts.Statements(mysql.MySQLDialect{}, mixed)
	require.NoError(t, err)
	assert.Equal(t, "ALTER TABLE products ADD FULLTEXT INDEX ftx_mixed_wd (sku, notes)", up[2])

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
	c3, _ := mustNew(t, "products", reweighted).Comment(pg)
	assert.NotEqual(t, c1, c3, "a weight changes the PostgreSQL document")

	m1, _ := base.Comment(my)
	m3, _ := mustNew(t, "products", reweighted).Comment(my)
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
	assert.Equal(t, "ALTER TABLE products DROP INDEX ftx_products_search_wa;\n\n"+
		"ALTER TABLE products DROP INDEX ftx_products_search_wb;\n\n"+
		"ALTER TABLE products DROP INDEX ftx_products_search;\n", string(down))

	assert.Error(t, fts.WriteMigration(dir, 1, "../escape", mysql.MySQLDialect{}, products))
}

// mustNew builds an index without registering it, for definitions that
// deliberately differ from a declared one.
func mustNew(t *testing.T, table string, spec fts.Spec) *fts.Index {
	t.Helper()
	idx, err := fts.NewIndex(table, spec)
	require.NoError(t, err)
	return idx
}

func TestMustIndexRegisters(t *testing.T) {
	spec := fts.Spec{Name: "ftx_registry_a", Columns: []fts.Column{{Name: "name"}}}
	a := fts.MustIndex("registry_t", spec)
	assert.Contains(t, fts.Registered(), a)
	assert.NotPanics(t, func() { fts.MustIndex("registry_t", spec) }, "the same definition twice is allowed")
	assert.Len(t, slicesFilter(fts.Registered(), "ftx_registry_a"), 1)

	other := spec
	other.Language = fts.English
	assert.PanicsWithError(t, "fts: invalid index: ftx_registry_a on registry_t is declared twice with different definitions",
		func() { fts.MustIndex("registry_t", other) })
	assert.NotPanics(t, func() { fts.MustIndex("other_table", other) }, "same name on another table")

	unregistered := mustNew(t, "registry_t", fts.Spec{Name: "ftx_registry_b", Columns: []fts.Column{{Name: "name"}}})
	assert.NotContains(t, fts.Registered(), unregistered)
}

func slicesFilter(indexes []*fts.Index, name string) []*fts.Index {
	var out []*fts.Index
	for _, idx := range indexes {
		if idx.Name() == name {
			out = append(out, idx)
		}
	}
	return out
}

func TestConcurrently(t *testing.T) {
	_, err := fts.NewIndex("products", fts.Spec{Name: "ftx_c", Columns: []fts.Column{{Name: "name"}}, Concurrently: true})
	assert.ErrorIs(t, err, fts.ErrInvalidIndex, "StoredVector rewrites the table: Concurrently needs ExpressionIndex")

	spec := fts.Spec{Name: "ftx_c", Columns: []fts.Column{{Name: "name"}}, Language: fts.Portuguese, Unaccent: true, Strategy: fts.ExpressionIndex}
	blocking := mustNew(t, "products", spec)
	spec.Concurrently = true
	idx := mustNew(t, "products", spec)

	pg := postgres.PostgresDialect{}
	c1, _ := blocking.Comment(pg)
	c2, _ := idx.Comment(pg)
	assert.Equal(t, c1, c2, "how the index is built is not part of what it holds")

	files, err := fts.Migrations(pg, idx)
	require.NoError(t, err)
	require.Len(t, files, 3, "configuration, then the concurrent index alone, then its comment")
	assert.Contains(t, files[0].Up, "CREATE TEXT SEARCH CONFIGURATION")
	assert.Equal(t, "SELECT 1;\n", files[0].Down, "the shared configuration is kept")
	assert.Equal(t, "CREATE INDEX CONCURRENTLY ftx_c ON products USING GIN ((to_tsvector('gofi_portuguese_unaccent'::regconfig, coalesce(name, ''))));\n", files[1].Up)
	assert.Equal(t, "DROP INDEX CONCURRENTLY IF EXISTS ftx_c;\n", files[1].Down)
	assert.Equal(t, "COMMENT ON INDEX ftx_c IS '"+c2+"';\n", files[2].Up)

	_, _, err = fts.Migration(pg, idx)
	assert.ErrorContains(t, err, "several files")

	dir := t.TempDir()
	require.NoError(t, fts.WriteMigration(dir, 7, "search", pg, idx))
	for _, f := range []string{"000007_search", "000008_search_2", "000009_search_3"} {
		for _, ext := range []string{".up.sql", ".down.sql"} {
			_, err := os.Stat(filepath.Join(dir, f+ext))
			assert.NoError(t, err, f+ext)
		}
	}

	_, _, err = fts.Statements(mysql.MySQLDialect{}, idx)
	assert.ErrorIs(t, err, fts.ErrInvalidIndex)
	assert.ErrorIs(t, err, mysql.ErrConcurrentIndex)
}

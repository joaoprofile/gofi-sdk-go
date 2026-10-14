package fts_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	_ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
	_ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
)

// Set GOFI_IT_POSTGRES_DSN (eval "$(.github/scripts/it-postgres.sh)") to run it.
func TestIntegration_PostgresFullText(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_POSTGRES_DSN not set")
	}
	fullTextScenario(t, connection.DriverPostgres, dsn,
		`CREATE TABLE it_fts_products (id bigint PRIMARY KEY, name text, description text)`)
}

// Set GOFI_IT_MYSQL_DSN (e.g. root:pw@tcp(localhost:3306)/it) to run it.
func TestIntegration_MySQLFullText(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_MYSQL_DSN not set")
	}
	fullTextScenario(t, connection.DriverMySQL, dsn,
		`CREATE TABLE it_fts_products (id bigint PRIMARY KEY, name varchar(200), description text)
		 DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci`)
}

type itProduct struct {
	ID int64 `db:"id"`
}

var itIndex = fts.MustIndex("it_fts_products", fts.Spec{
	Name:     "ftx_it_fts_products",
	Columns:  []fts.Column{{Name: "name", Weight: fts.A}, {Name: "description", Weight: fts.B}},
	Language: fts.Portuguese,
	Unaccent: true,
	Synonyms: [][]string{{"notebook", "laptop"}},
	Suggest:  true,
})

func fullTextScenario(t *testing.T, driver connection.DriverName, dsn, createTable string) {
	ctx := context.Background()
	conn, err := connection.NewConnection(connection.Config{Driver: driver, DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	db, d := conn.DB(), conn.Dialect()

	exec := func(stmts ...string) {
		t.Helper()
		for _, s := range stmts {
			_, err := db.ExecContext(ctx, s)
			require.NoError(t, err, s)
		}
	}
	exec("DROP TABLE IF EXISTS it_fts_products", createTable)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS it_fts_products") })
	exec(`INSERT INTO it_fts_products (id, name, description) VALUES
		(1, 'Cafeteira Elétrica', 'máquina de café expresso'),
		(2, 'Notebook Gamer', 'placa de vídeo dedicada'),
		(3, 'Café em grãos', 'café especial torrado'),
		(4, 'Mouse sem fio', 'para notebook e desktop')`)

	search := func(s fts.Search) ([]int64, error) {
		rows, err := sqln.FindFromCriteria[itProduct](ctx,
			criteria.From("it_fts_products", "p").Select("p.id").Where(criteria.Match(s)).OrderBy(criteria.Asc("p.id")),
		).WithConnection(conn).List()
		ids := make([]int64, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		return ids, err
	}

	// No index: the search does not run, the connection says why.
	_, err = search(itIndex.Search("cafe"))
	require.ErrorIs(t, err, fts.ErrIndexMissing)

	up, down, err := fts.Statements(d, itIndex)
	require.NoError(t, err)
	exec(up...)
	require.NoError(t, fts.Check(ctx, db, d, itIndex))

	for _, tt := range []struct {
		name string
		s    fts.Search
		want []int64
	}{
		{"word, accent-insensitive", itIndex.Search("cafe"), []int64{1, 3}},
		{"accented query", itIndex.Search("ELÉTRICA"), []int64{1}},
		{"exclusion", itIndex.Search("notebook -mouse"), []int64{2}},
		{"alternatives", itIndex.Search("cafe OR mouse"), []int64{1, 3, 4}},
		{"phrase with a stopword", itIndex.Search(`"maquina de cafe"`), []int64{1}},
		{"phrase out of order", itIndex.Search(`"cafe maquina"`), []int64{}},
		{"prefix", itIndex.Search("note gam").Mode(fts.Prefix), []int64{2}},
		{"any term", itIndex.Search("torrado dedicada").Mode(fts.AnyTerm), []int64{2, 3}},
		{"short words dropped on MySQL, ignored on PostgreSQL", itIndex.Search("de notebook e"), []int64{2, 4}},
		{"synonym", itIndex.Search("laptop"), []int64{2, 4}},
		{"excluded synonym", itIndex.Search("gamer -laptop"), []int64{}},
		// máquina(1) de(2) café(3) expresso(4): 3 positions apart, in any order.
		{"near enough", itIndex.Search(`"expresso maquina"~3`), []int64{1}},
		{"too far", itIndex.Search(`"maquina expresso"~2`), []int64{}},
		{"adjacent", itIndex.Search(`"cafe expresso"~1`), []int64{1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ids, err := search(tt.s)
			require.NoError(t, err)
			assert.Equal(t, tt.want, append([]int64{}, ids...))
		})
	}

	// Ranked and paged: the relevance alias is a sort column.
	s := itIndex.Search("cafe")
	page, err := sqln.FindFromCriteria[itProduct](ctx,
		criteria.From("it_fts_products", "p").Select("p.id").SelectRelevance(s, "relevance").Where(criteria.Match(s)),
	).WithConnection(conn).WithPage(pagination.NewPageRequest(0, 1, []pagination.Sort{pagination.NewSort("relevance", pagination.DESC)})).PagedList()
	require.NoError(t, err)
	assert.EqualValues(t, 2, page.TotalElements)
	assert.Equal(t, []itProduct{{ID: 3}}, page.Content, "café in the name and twice in the description ranks first")

	// Terms in different weighted columns still rank: "cafe" is in the
	// description of 1, "eletrica" in its name.
	type scored struct {
		ID        int64   `db:"id"`
		Relevance float64 `db:"relevance"`
	}
	both := itIndex.Search("cafe eletrica")
	hits, err := sqln.FindFromCriteria[scored](ctx,
		criteria.From("it_fts_products", "p").Select("p.id").SelectRelevance(both, "relevance").Where(criteria.Match(both)),
	).WithConnection(conn).List()
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Positive(t, hits[0].Relevance)

	// Highlight: the same marked words on both databases, scanned into
	// fts.Highlight and escaped by HTML.
	type snippet struct {
		ID      int64         `db:"id"`
		Snippet fts.Highlight `db:"snippet"`
	}
	highlight := func(s fts.Search, column string) map[int64]string {
		t.Helper()
		rows, err := sqln.FindFromCriteria[snippet](ctx,
			criteria.From("it_fts_products", "p").Select("p.id").SelectHighlight(s, column, "snippet").
				Where(criteria.Match(s)).OrderBy(criteria.Asc("p.id")),
		).WithConnection(conn).List()
		require.NoError(t, err)
		out := map[int64]string{}
		for _, r := range rows {
			out[r.ID] = r.Snippet.HTML()
		}
		return out
	}
	assert.Equal(t, map[int64]string{1: "Cafeteira Elétrica", 3: "<mark>Café</mark> em grãos"},
		highlight(itIndex.Search("cafe"), "name"), "whole words: cafeteira is another word")
	assert.Equal(t, map[int64]string{3: "<mark>café</mark> especial <mark>torrado</mark>"},
		highlight(itIndex.Search("cafe torrado"), "description"))
	assert.Equal(t, map[int64]string{2: "<mark>Notebook</mark> Gamer", 4: "Mouse sem fio"},
		highlight(itIndex.Search("note").Mode(fts.Prefix), "name"))
	assert.Equal(t, map[int64]string{2: "<mark>Notebook</mark> Gamer", 4: "Mouse sem fio"},
		highlight(itIndex.Search("laptop"), "name"), "synonyms are marked")

	// Did you mean: the same correction on both databases.
	n, err := fts.RefreshLexicon(ctx, db, d, itIndex)
	require.NoError(t, err)
	assert.Positive(t, n)
	for term, want := range map[string]string{
		"cafeteria eletrica": "cafeteira eletrica",
		"notbook -mause":     "notebook -mause", // negated words are not corrected
		`"mause" gamer`:      `"mouse" gamer`,
	} {
		got, changed, err := fts.Suggest(ctx, db, d, itIndex.Search(term))
		require.NoError(t, err, term)
		assert.True(t, changed, term)
		assert.Equal(t, want, got.Term(), term)
	}
	for _, term := range []string{"cafe", "elétrica", "xyzzyqw", "de"} {
		got, changed, err := fts.Suggest(ctx, db, d, itIndex.Search(term))
		require.NoError(t, err, term)
		assert.False(t, changed, term)
		assert.Equal(t, term, got.Term())
	}
	fixed, _, err := fts.Suggest(ctx, db, d, itIndex.Search("cafeteria"))
	require.NoError(t, err)
	ids, err := search(fixed)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids, "the corrected search finds what the typo missed")

	// The plan reads the index.
	query, args, err := criteria.From("it_fts_products", "p").Select("p.id").
		Where(criteria.Match(s)).OrderBy(criteria.ByRelevance(s)).Build(d)
	require.NoError(t, err)
	assert.Contains(t, explain(t, db, driver, query, args), "ftx_it_fts_products")

	// A changed definition is detected, never silently searched.
	changed, err := fts.NewIndex("it_fts_products", fts.Spec{Name: "ftx_it_fts_products", Columns: []fts.Column{{Name: "name"}}})
	require.NoError(t, err)
	assert.ErrorIs(t, fts.Check(ctx, db, d, changed), fts.ErrIndexOutdated)

	// Down drops it; Create (the Ensure policy) brings it back.
	exec(down...)
	assert.ErrorIs(t, fts.Check(ctx, db, d, itIndex), fts.ErrIndexMissing)
	require.NoError(t, fts.Create(ctx, db, d, itIndex))
	ids, err = search(itIndex.Search("cafe"))
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, ids)
}

// explain returns the plan of query; PostgreSQL is kept off sequential scans,
// which win on a four-row table.
func explain(t *testing.T, db *sql.DB, driver connection.DriverName, query string, args []any) string {
	ctx := context.Background()
	c, err := db.Conn(ctx)
	require.NoError(t, err)
	defer c.Close()
	if driver == connection.DriverPostgres {
		_, err = c.ExecContext(ctx, "SET enable_seqscan = off")
		require.NoError(t, err)
		defer c.ExecContext(ctx, "RESET enable_seqscan")
	}
	rows, err := c.QueryContext(ctx, "EXPLAIN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	cols, _ := rows.Columns()
	var plan []string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		for _, v := range vals {
			plan = append(plan, v.String)
		}
	}
	require.NoError(t, rows.Err())
	return strings.Join(plan, " ")
}

func TestIntegration_PostgresBootMigratesAndChecks(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_POSTGRES_DSN not set")
	}
	bootScenario(t, connection.DriverPostgres, dsn, `CREATE TABLE it_fts_boot (id bigint PRIMARY KEY, name text)`,
		fts.Spec{Name: "ftx_it_fts_boot", Columns: []fts.Column{{Name: "name"}}, Language: fts.Portuguese})
}

// CREATE INDEX CONCURRENTLY cannot run inside a transaction block: it must
// reach golang-migrate in a file of its own.
func TestIntegration_PostgresConcurrentMigration(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_POSTGRES_DSN not set")
	}
	bootScenario(t, connection.DriverPostgres, dsn, `CREATE TABLE it_fts_boot (id bigint PRIMARY KEY, name text)`,
		fts.Spec{Name: "ftx_it_fts_boot", Columns: []fts.Column{{Name: "name"}}, Language: fts.Portuguese,
			Unaccent: true, Strategy: fts.ExpressionIndex, Concurrently: true})
}

func TestIntegration_MySQLBootMigratesAndChecks(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_MYSQL_DSN not set")
	}
	bootScenario(t, connection.DriverMySQL, dsn,
		`CREATE TABLE it_fts_boot (id bigint PRIMARY KEY, name varchar(200), notes text) DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci`,
		fts.Spec{Name: "ftx_it_fts_boot", Columns: []fts.Column{{Name: "name", Weight: fts.A}, {Name: "notes", Weight: fts.C}},
			Language: fts.Portuguese, Unaccent: true})
}

// Unaccent on MySQL needs an accent-insensitive collation: the index works,
// but "cafe" would not find "café", so the check refuses it.
func TestIntegration_MySQLAccentSensitiveCollation(t *testing.T) {
	dsn := os.Getenv("GOFI_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_MYSQL_DSN not set")
	}
	ctx := context.Background()
	conn, err := connection.NewConnection(connection.Config{Driver: connection.DriverMySQL, DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	db, d := conn.DB(), conn.Dialect()
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS it_fts_cs")
	_, err = db.ExecContext(ctx, "CREATE TABLE it_fts_cs (id int PRIMARY KEY, name varchar(100)) DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_as_cs")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS it_fts_cs") })

	idx, err := fts.NewIndex("it_fts_cs", fts.Spec{Name: "ftx_it_fts_cs", Columns: []fts.Column{{Name: "name"}}, Unaccent: true})
	require.NoError(t, err)
	err = fts.Create(ctx, db, d, idx)
	assert.ErrorIs(t, err, fts.ErrServerConfig)
	assert.ErrorContains(t, err, "collation of name is utf8mb4_0900_as_cs")
}

// bootScenario opens a connection the way gofi's database component does:
// migrations first (a multi-statement file, then the index rendered by
// WriteMigration), then the startup check of the declared index.
func bootScenario(t *testing.T, driver connection.DriverName, dsn, createTable string, spec fts.Spec) {
	ctx := context.Background()
	d, ok := connection.GetDriver(driver)
	require.True(t, ok)
	idx, err := fts.NewIndex("it_fts_boot", spec)
	require.NoError(t, err)

	reset := func() {
		raw, err := d.Open(connection.Config{DSN: dsn})
		require.NoError(t, err)
		defer raw.Close()
		for _, s := range []string{"DROP TABLE IF EXISTS it_fts_boot", "DROP TABLE IF EXISTS schema_migrations"} {
			_, err := raw.ExecContext(ctx, s)
			require.NoError(t, err, s)
		}
	}
	reset()
	t.Cleanup(reset)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/000001_boot_table.up.sql",
		[]byte(createTable+";\nINSERT INTO it_fts_boot (id, name) VALUES (1, 'Café Expresso'), (2, 'Chá Verde');\n"), 0o600))
	require.NoError(t, os.WriteFile(dir+"/000001_boot_table.down.sql", []byte("DROP TABLE it_fts_boot;\n"), 0o600))
	require.NoError(t, fts.WriteMigration(dir, 2, "boot_search", d.Dialect(), idx))

	cfg := connection.Config{Driver: driver, DSN: dsn, FullText: connection.FullTextConfig{Indexes: []*fts.Index{idx}}}
	_, err = connection.NewConnection(cfg)
	require.ErrorIs(t, err, fts.ErrIndexMissing, "without migrations the startup fails")

	conn, err := connection.NewConnection(cfg, connection.WithMigrations(migrate.Config{Path: dir}))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	rows, err := sqln.FindFromCriteria[itProduct](ctx,
		criteria.From("it_fts_boot", "b").Select("b.id").Where(criteria.Match(idx.Search("cafe"))),
	).WithConnection(conn).List()
	require.NoError(t, err)
	assert.Equal(t, []itProduct{{ID: 1}}, rows)
}

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

	// The plan reads the index.
	query, args, err := criteria.From("it_fts_products", "p").Select("p.id").
		Where(criteria.Match(s)).OrderBy(criteria.ByRelevance(s)).Build(d)
	require.NoError(t, err)
	assert.Contains(t, explain(t, db, driver, query, args), "ftx_it_fts_products")

	// A changed definition is detected, never silently searched.
	changed := fts.MustIndex("it_fts_products", fts.Spec{Name: "ftx_it_fts_products", Columns: []fts.Column{{Name: "name"}}})
	assert.ErrorIs(t, fts.Check(ctx, db, d, changed), fts.ErrIndexOutdated)

	// Down drops it; Create (the Ensure policy) brings it back.
	exec(down...)
	assert.ErrorIs(t, fts.Check(ctx, db, d, itIndex), fts.ErrIndexMissing)
	require.NoError(t, fts.Create(ctx, db, d, itIndex))
	ids, err := search(itIndex.Search("cafe"))
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

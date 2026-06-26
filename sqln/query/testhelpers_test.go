package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

const testDriver = "query-testdriver"

var registerOnce sync.Once

func TestMain(m *testing.M) {
	logging.NewLogger("query-test")
	os.Exit(m.Run())
}

// Fake database/sql driver

func initDriver() {
	registerOnce.Do(func() {
		sql.Register(testDriver, &fakeSQLDriver{})
	})
}

type fakeSQLDriver struct{}

func (d *fakeSQLDriver) Open(_ string) (driver.Conn, error) {
	return &fakeConn{}, nil
}

type fakeConn struct{}

func (c *fakeConn) Prepare(_ string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *fakeConn) Close() error                          { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)             { return &fakeTx{}, nil }

type fakeStmt struct{}

func (s *fakeStmt) Close() error                                 { return nil }
func (s *fakeStmt) NumInput() int                                { return -1 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) { return driver.RowsAffected(1), nil }
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error)  { return &fakeRows{}, nil }

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeRows struct{ done bool }

func (r *fakeRows) Columns() []string { return []string{"col"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "value"
	return nil
}

// fakeConnDriver implements connection.Driver — used only for connection package tests
// that need a Driver; the query package tests use *sql.DB directly.
type fakeConnDriver struct{}

func (d fakeConnDriver) Name() connection.DriverName    { return connection.DriverName(testDriver) }
func (d fakeConnDriver) DSN(connection.Settings) string { return "" }
func (d fakeConnDriver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open(testDriver, cfg.DSN)
}
func (d fakeConnDriver) ParseError(err error) error   { return err }
func (d fakeConnDriver) Dialect() sqln_driver.Dialect { return fakeDialect{} }

type fakeDialect struct{}

func (fakeDialect) Param(_ int) string                              { return "?" }
func (fakeDialect) Like(f, p string) string                         { return f + " LIKE " + p }
func (fakeDialect) NotLike(f, p string) string                      { return f + " NOT LIKE " + p }
func (fakeDialect) BuildPagination(q, _ string, _, _ uint16) string { return q }
func (fakeDialect) BuildCount(q string) string                      { return "SELECT COUNT(*) FROM (" + q + ") t" }

// Helpers

// openDB returns an open *sql.DB backed by the fake driver.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	initDriver()
	db, err := sql.Open(testDriver, "ok")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// txContext injects an active *sql.Tx into ctx using the shared context key.
func txContext(t *testing.T, db *sql.DB) (context.Context, *sql.Tx) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}
	ctx := context.WithValue(context.Background(), connection.SqlTxContextKey, tx)
	return ctx, tx
}

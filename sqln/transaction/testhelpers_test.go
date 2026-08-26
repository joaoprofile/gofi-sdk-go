package transaction

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

const testDriver = "tx-testdriver"

var registerOnce sync.Once

func TestMain(m *testing.M) {
	logging.NewLogger("transaction-test")
	os.Exit(m.Run())
}

// Fake database/sql driver

func initDriver() {
	registerOnce.Do(func() {
		sql.Register(testDriver, &fakeSQLDriver{})
		connection.RegisterDriver(fakeConnDriver{})
	})
}

type fakeSQLDriver struct{}

func (d *fakeSQLDriver) Open(name string) (driver.Conn, error) {
	switch name {
	case "fail-begin":
		return &failBeginConn{}, nil
	case "fail-commit":
		return &failCommitConn{}, nil
	case "fail-rollback":
		return &failRollbackConn{}, nil
	default:
		return &fakeConn{}, nil
	}
}

var beginCount atomic.Int32

// fakeConn — all operations succeed
type fakeConn struct{}

func (c *fakeConn) Prepare(_ string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *fakeConn) Close() error                          { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	beginCount.Add(1)
	return &fakeTx{}, nil
}

// failBeginConn — Begin always fails
type failBeginConn struct{ fakeConn }

func (c *failBeginConn) Begin() (driver.Tx, error) { return nil, errors.New("begin failed") }

// failCommitConn — Begin succeeds, Commit fails
type failCommitConn struct{ fakeConn }

func (c *failCommitConn) Begin() (driver.Tx, error) { return &failCommitTx{}, nil }

type failCommitTx struct{ fakeTx }

func (t *failCommitTx) Commit() error { return errors.New("commit failed") }

// failRollbackConn — Begin succeeds, Rollback fails
type failRollbackConn struct{ fakeConn }

func (c *failRollbackConn) Begin() (driver.Tx, error) { return &failRollbackTx{}, nil }

type failRollbackTx struct{ fakeTx }

func (t *failRollbackTx) Rollback() error { return errors.New("rollback failed") }

type fakeStmt struct{}

func (s *fakeStmt) Close() error                                 { return nil }
func (s *fakeStmt) NumInput() int                                { return -1 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) { return driver.RowsAffected(1), nil }
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error)  { return &fakeRows{}, nil }

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeRows struct{}

func (r *fakeRows) Columns() []string           { return nil }
func (r *fakeRows) Close() error                { return nil }
func (r *fakeRows) Next(_ []driver.Value) error { return io.EOF }

// fakeConnDriver — implements connection.Driver

type fakeConnDriver struct{}

func (d fakeConnDriver) Name() connection.DriverName    { return connection.DriverName(testDriver) }
func (d fakeConnDriver) DSN(connection.Settings) string { return "" }
func (d fakeConnDriver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open(testDriver, cfg.DSN)
}
func (d fakeConnDriver) ParseError(err error) error   { return err }
func (d fakeConnDriver) Dialect() sqln_driver.Dialect { return fakeDialect{} }

type fakeDialect struct{}

func (fakeDialect) Param(_ int) string                                     { return "?" }
func (fakeDialect) Like(f, p string) string                                { return f + " LIKE " + p }
func (fakeDialect) NotLike(f, p string) string                             { return f + " NOT LIKE " + p }
func (fakeDialect) BuildPagination(q, _ string, _ uint16, _ uint64) string { return q }
func (fakeDialect) BuildCount(q string) string                             { return "SELECT COUNT(*) FROM (" + q + ") t" }

// Helpers

// setupGlobal sets the global connection to a new fake DB with the given DSN.
func setupGlobal(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	initDriver()
	connection.ResetGlobalForTest()

	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriver),
		DSN:    dsn,
	})
	if err != nil {
		t.Fatalf("setup: NewConnection(%q): %v", dsn, err)
	}
	connection.SetGlobal(conn)
	t.Cleanup(func() {
		connection.ResetGlobalForTest()
		_ = conn.Close()
	})
	return conn.DB()
}

// rawDB opens a *sql.DB directly (bypasses global) for cases where we need
// a DB with a specific behavior (e.g., fail-begin) without touching the global.
func rawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	initDriver()
	db, err := sql.Open(testDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", dsn, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

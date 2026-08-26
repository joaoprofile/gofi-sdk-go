package connection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// TestMain inicializa o logger antes de todos os testes do pacote.
func TestMain(m *testing.M) {
	logging.NewLogger("connection-test")
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Fake SQL driver (database/sql level)
// ---------------------------------------------------------------------------

var registerOnce sync.Once

const testDriverDSN = "testconn"

func initTestSQLDriver() {
	registerOnce.Do(func() {
		sql.Register(testDriverDSN, &fakeSQLDriver{})
	})
}

type fakeSQLDriver struct{}

func (d *fakeSQLDriver) Open(name string) (driver.Conn, error) {
	switch name {
	case "fail-open":
		return nil, errors.New("cannot open connection")
	case "fail-ping":
		return &failPingConn{}, nil
	default:
		return &fakeConn{}, nil
	}
}

// fakeConn — a connection that always succeeds
type fakeConn struct{}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *fakeConn) Close() error                              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)                 { return &fakeTx{}, nil }

// failPingConn — a connection that implements Pinger and returns an error
type failPingConn struct{ fakeConn }

func (c *failPingConn) Ping(_ context.Context) error { return errors.New("ping failed") }

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

// ---------------------------------------------------------------------------
// Fake connection.Driver (gofi connection level)
// Uses cfg.DSN so different scenarios can be exercised (fail-open, fail-ping).
// ---------------------------------------------------------------------------

type fakeConnDriver struct{}

func (d fakeConnDriver) Name() DriverName    { return DriverName(testDriverDSN) }
func (d fakeConnDriver) DSN(Settings) string { return "" }
func (d fakeConnDriver) Open(cfg Config) (*sql.DB, error) {
	return sql.Open(testDriverDSN, cfg.DSN)
}
func (d fakeConnDriver) ParseError(err error) error { return err }
func (d fakeConnDriver) Dialect() sqln_driver.Dialect {
	return fakeDialect{}
}

type fakeDialect struct{}

func (fakeDialect) Param(_ int) string                                     { return "?" }
func (fakeDialect) Like(field, param string) string                        { return field + " LIKE " + param }
func (fakeDialect) NotLike(field, param string) string                     { return field + " NOT LIKE " + param }
func (fakeDialect) BuildPagination(q, _ string, _ uint16, _ uint64) string { return q }
func (fakeDialect) BuildCount(q string) string                             { return "SELECT COUNT(*) FROM (" + q + ") t" }

// ---------------------------------------------------------------------------
// Global state reset — tests need clean state between runs.
// ---------------------------------------------------------------------------

func resetGlobal() {
	ResetGlobalForTest()
}

// mustOpenTestDB opens a *sql.DB through the fake driver for direct use in tests.
func mustOpenTestDB(dsn string) *sql.DB {
	initTestSQLDriver()
	db, err := sql.Open(testDriverDSN, dsn)
	if err != nil {
		panic(err)
	}
	return db
}

var registerDriverOnce sync.Once

func registerFakeDriverOnce() {
	registerDriverOnce.Do(func() {
		initTestSQLDriver()
		RegisterDriver(fakeConnDriver{})
	})
}

// newTestConnection builds a *Connection backed by the fake driver for tests.
func newTestConnection(dsn string) *Connection {
	registerFakeDriverOnce()
	conn, err := NewConnection(Config{Driver: DriverName(testDriverDSN), DSN: dsn})
	if err != nil {
		panic(err)
	}
	return conn
}

// Silences the "declared and not used" testing import when TestMain is the only function.
var _ testing.TB

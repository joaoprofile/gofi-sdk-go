package sqln

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln/cache"
	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

const testDriverName = "sqln-manager-testdriver"

var registerOnce sync.Once

func TestMain(m *testing.M) {
	logging.NewLogger("sqln-manager-test")
	os.Exit(m.Run())
}

// Fake database/sql driver — supports multiple row shapes via DSN

func initDriver() {
	registerOnce.Do(func() {
		sql.Register(testDriverName, &fakeDriver{})
		connection.RegisterDriver(fakeConnDriver{})
	})
}

// fakeDriver dispatches on DSN to produce different query results.
type fakeDriver struct{}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	switch name {
	case "struct-rows":
		return &fakeConn{
			cols: []string{"id", "name"},
			rows: [][]driver.Value{{int64(1), "Emilia"}},
		}, nil
	case "count-rows":
		// Returns a single int64 column — used for both COUNT queries (pageTotal)
		// and scalar list queries (fetchPagedList with scalarItem = int64).
		return &fakeConn{
			cols: []string{"count"},
			rows: [][]driver.Value{{int64(5)}},
		}, nil
	default: // "ok", "no-rows", etc.
		return &fakeConn{cols: []string{"id"}, rows: nil}, nil
	}
}

type fakeConn struct {
	cols []string
	rows [][]driver.Value
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	recordQuery(query)
	return &fakeStmt{cols: c.cols, rows: c.rows}, nil
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{}, nil }

type fakeStmt struct {
	cols []string
	rows [][]driver.Value
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return &fakeRows{cols: s.cols, rows: s.rows}, nil
}

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

// Query recorder — without it the fake cannot assert WHICH SQL was issued, and
// the count hook is only provable by looking at the query that reached the driver.

var (
	recordedMu      sync.Mutex
	recordedQueries []string
)

var lastPaginationOffset uint64

func recordPaginationOffset(offset uint64) {
	recordedMu.Lock()
	defer recordedMu.Unlock()
	lastPaginationOffset = offset
}

func paginationOffset(t *testing.T) uint64 {
	t.Helper()
	recordedMu.Lock()
	defer recordedMu.Unlock()
	return lastPaginationOffset
}

func recordQuery(query string) {
	recordedMu.Lock()
	defer recordedMu.Unlock()
	recordedQueries = append(recordedQueries, query)
}

func resetRecordedQueries(t *testing.T) {
	t.Helper()
	recordedMu.Lock()
	defer recordedMu.Unlock()
	recordedQueries = nil
}

func recorded(t *testing.T) []string {
	t.Helper()
	recordedMu.Lock()
	defer recordedMu.Unlock()
	return append([]string(nil), recordedQueries...)
}

// fakeConnDriver implements connection.Driver
type fakeConnDriver struct{}

func (d fakeConnDriver) Name() connection.DriverName    { return connection.DriverName(testDriverName) }
func (d fakeConnDriver) DSN(connection.Settings) string { return "" }
func (d fakeConnDriver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open(testDriverName, cfg.DSN)
}
func (d fakeConnDriver) ParseError(err error) error   { return err }
func (d fakeConnDriver) Dialect() sqln_driver.Dialect { return fakeDialect{} }

type fakeDialect struct{}

func (fakeDialect) Param(_ int) string         { return "?" }
func (fakeDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (fakeDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }

// Records the offset it was handed: the pagination bug lives in how the manager
// computes offset, not in how a dialect renders it, so the value received is
// what a test has to be able to assert on. The query itself passes through —
// the fake driver returns the same rows regardless.
func (fakeDialect) BuildPagination(q, order string, limit uint16, offset uint64) string {
	recordPaginationOffset(offset)
	return q
}
func (fakeDialect) BuildCount(q string) string {
	return "SELECT COUNT(*) FROM (" + q + ") t"
}

// Test DB helpers

// openDB opens a *sql.DB with the given DSN via the fake driver.
func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	initDriver()
	db, err := sql.Open(testDriverName, dsn)
	if err != nil {
		t.Fatalf("openDB(%q): %v", dsn, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// setupGlobal sets the global connection with the given DSN and resets on cleanup.
func setupGlobal(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	initDriver()
	connection.ResetGlobalForTest()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    dsn,
	})
	if err != nil {
		t.Fatalf("setupGlobal: NewConnection: %v", err)
	}
	connection.SetGlobal(conn)
	t.Cleanup(func() {
		connection.ResetGlobalForTest()
		_ = conn.Close()
	})
	return conn.DB()
}

// Model types used across tests

// mappedItem has gofi tags so GetMappedCols returns its fields.
type mappedItem struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

// scalarItem is a simple type (int64) — GetContentList uses rows.Scan(&value).
type scalarItem = int64

// newTestCache creates a Cache without Redis — operations fall through gracefully.
func newTestCache[T any]() *cache.Cache[T] {
	return cache.NewCache[T]("test-key", 0)
}

// txContext injects a *sql.Tx into ctx.
func txContext(t *testing.T, db *sql.DB) context.Context {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin: %v", err)
	}
	ctx := context.WithValue(context.Background(), SqlTxContextKey, tx)
	t.Cleanup(func() { _ = tx.Rollback() })
	return ctx
}

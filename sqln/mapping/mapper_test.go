package mapping

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fake database/sql driver for rows simulation

const testDriver = "mapping-testdriver"

func init() {
	sql.Register(testDriver, &fakeDriver{})
}

type fakeDriver struct{}

func (d *fakeDriver) Open(_ string) (driver.Conn, error) { return &fakeConn{}, nil }

type fakeConn struct{}

func (c *fakeConn) Prepare(_ string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *fakeConn) Close() error                          { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)             { return &fakeTx{}, nil }

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	rows [][]driver.Value
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return s.rowSet(), nil
}
func (s *fakeStmt) rowSet() driver.Rows { return &fakeRows{rows: s.rows} }

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

// rowsFromValues builds a *sql.Rows that yields the provided rows.
// cols must match the number of values per row.
func rowsFromValues(t *testing.T, cols []string, data [][]driver.Value) *sql.Rows {
	t.Helper()
	db, err := sql.Open(testDriver, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// We need to inject our fake rows. Use a custom driver per call.
	driverName := t.Name()
	sql.Register(driverName, &inlineDriver{cols: cols, data: data})
	db2, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open inline: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	rows, err := db2.Query("SELECT 1")
	if err != nil {
		t.Fatalf("db.Query: %v", err)
	}
	return rows
}

// inlineDriver lets each test register its own driver with fixed data.
type inlineDriver struct {
	cols []string
	data [][]driver.Value
}

func (d *inlineDriver) Open(_ string) (driver.Conn, error) {
	return &inlineConn{cols: d.cols, data: d.data}, nil
}

type inlineConn struct {
	cols []string
	data [][]driver.Value
}

func (c *inlineConn) Prepare(_ string) (driver.Stmt, error) {
	return &inlineStmt{cols: c.cols, data: c.data}, nil
}
func (c *inlineConn) Close() error              { return nil }
func (c *inlineConn) Begin() (driver.Tx, error) { return &fakeTx{}, nil }

type inlineStmt struct {
	cols []string
	data [][]driver.Value
}

func (s *inlineStmt) Close() error  { return nil }
func (s *inlineStmt) NumInput() int { return -1 }
func (s *inlineStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (s *inlineStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return &fakeRows{cols: s.cols, rows: s.data}, nil
}

// errDriver returns an error on Next after the first row.
type errRows struct {
	called bool
}

func (r *errRows) Columns() []string { return []string{"id"} }
func (r *errRows) Close() error      { return nil }
func (r *errRows) Next(dest []driver.Value) error {
	if !r.called {
		r.called = true
		dest[0] = int64(1)
		return nil
	}
	return errors.New("scan error")
}

// sqlRowsFromDriverRows wraps a driver.Rows into *sql.Rows via a helper driver.
type wrapDriver struct{ rows driver.Rows }

func (d *wrapDriver) Open(_ string) (driver.Conn, error) {
	return &wrapConn{rows: d.rows}, nil
}

type wrapConn struct{ rows driver.Rows }

func (c *wrapConn) Prepare(_ string) (driver.Stmt, error) {
	return &wrapStmt{rows: c.rows}, nil
}
func (c *wrapConn) Close() error              { return nil }
func (c *wrapConn) Begin() (driver.Tx, error) { return &fakeTx{}, nil }

type wrapStmt struct{ rows driver.Rows }

func (s *wrapStmt) Close() error  { return nil }
func (s *wrapStmt) NumInput() int { return -1 }
func (s *wrapStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}
func (s *wrapStmt) Query(_ []driver.Value) (driver.Rows, error) { return s.rows, nil }

var wrapSeq int

func openWithRows(t *testing.T, drows driver.Rows) *sql.Rows {
	t.Helper()
	wrapSeq++
	name := t.Name()
	sql.Register(name, &wrapDriver{rows: drows})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open wrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows, err := db.Query("")
	if err != nil {
		t.Fatalf("db.Query wrap: %v", err)
	}
	return rows
}

// IsSimpleType

func TestIsSimpleType_String(t *testing.T) {
	assert.True(t, IsSimpleType("hello"))
}

func TestIsSimpleType_Int(t *testing.T) {
	assert.True(t, IsSimpleType(42))
}

func TestIsSimpleType_Float(t *testing.T) {
	assert.True(t, IsSimpleType(3.14))
}

func TestIsSimpleType_Bool(t *testing.T) {
	assert.True(t, IsSimpleType(true))
}

func TestIsSimpleType_Struct(t *testing.T) {
	type S struct{ X int }
	assert.False(t, IsSimpleType(S{}))
}

func TestIsSimpleType_Pointer(t *testing.T) {
	x := 1
	assert.False(t, IsSimpleType(&x))
}

// GetMappedCols

type mappedModel struct {
	ID   int    `db:"id"`
	Name string `db:"name"`
}

type noTagModel struct {
	ID   int
	Name string
}

type sliceModel struct {
	Tags []string `db:"tags"`
}

func TestGetMappedCols_WithGofiTags_ReturnsAddresses(t *testing.T) {
	m := &mappedModel{ID: 1, Name: "test"}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2)
}

func TestGetMappedCols_NoTags_ReturnsEmpty(t *testing.T) {
	m := &noTagModel{}
	cols := GetMappedCols(m)
	assert.Empty(t, cols)
}

func TestGetMappedCols_NonPointer_WrapsInPointer(t *testing.T) {
	m := mappedModel{ID: 1, Name: "test"}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2)
}

func TestGetMappedCols_SliceField_UsesPqArray(t *testing.T) {
	m := &sliceModel{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 1)
}

func TestGetMappedCols_NonStructNonPointer_ReturnsAddr(t *testing.T) {
	// Calling with a primitive — code path: modelValue.Kind() != reflect.Struct
	val := 42
	cols := GetMappedCols(&val)
	require.Len(t, cols, 1)
}

// GetContentList — simple type (scalar rows)

func TestGetContentList_SimpleType_Empty(t *testing.T) {
	rows := openWithRows(t, &fakeRows{cols: []string{"id"}, rows: nil})
	defer rows.Close()

	list, err := GetContentList[int](rows)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestGetContentList_SimpleType_SingleRow(t *testing.T) {
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id"},
		rows: [][]driver.Value{{int64(99)}},
	})
	defer rows.Close()

	list, err := GetContentList[int64](rows)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(99), list[0])
}

func TestGetContentList_SimpleType_MultipleRows(t *testing.T) {
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id"},
		rows: [][]driver.Value{{int64(1)}, {int64(2)}, {int64(3)}},
	})
	defer rows.Close()

	list, err := GetContentList[int64](rows)
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

// GetContentList — struct type (mapped rows)

func TestGetContentList_StructType_SingleRow(t *testing.T) {
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id", "name"},
		rows: [][]driver.Value{{int64(1), "Emilia"}},
	})
	defer rows.Close()

	list, err := GetContentList[mappedModel](rows)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(1), int64(list[0].ID))
}

// GetContentList — scan error propagation

func TestGetContentList_ScanError_ReturnsError(t *testing.T) {
	rows := openWithRows(t, &errRows{})
	defer rows.Close()

	_, err := GetContentList[int64](rows)
	assert.Error(t, err)
}

func TestGetContentList_StructType_ScanError_ReturnsError(t *testing.T) {
	// noTagModel has no `gofi` tags → GetMappedCols returns empty slice
	// rows has 1 column → Scan(0 args) returns "expected 1 destination arguments, not 0"
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id"},
		rows: [][]driver.Value{{int64(1)}},
	})
	defer rows.Close()

	_, err := GetContentList[noTagModel](rows)
	assert.Error(t, err)
}

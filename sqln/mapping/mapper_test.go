package mapping

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

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

// Nested value objects

type pricingVO struct {
	Price float64 `db:"price"`
}

type productWithVO struct {
	ID    int64     `db:"id"`
	Name  string    `db:"name"`
	Price pricingVO `db:"price"`
}

func TestGetMappedCols_NestedVO_ExpandsToLeaves(t *testing.T) {
	m := &productWithVO{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 3) // id, name, price → Price.Price
}

type addressVO struct {
	Street  string `db:"street"`
	City    string `db:"city"`
	ZipCode string `db:"zip_code"`
}

type customerWithAddress struct {
	ID      int64     `db:"id"`
	Name    string    `db:"name"`
	Address addressVO `db:"address"`
}

func TestGetMappedCols_NestedVO_MultipleSubFields(t *testing.T) {
	m := &customerWithAddress{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 5) // id, name, street, city, zip_code
}

type deepLeaf struct {
	Value string `db:"value"`
}

type midLevel struct {
	Deep deepLeaf `db:"deep"`
}

type rootLevel struct {
	ID  int64    `db:"id"`
	Mid midLevel `db:"mid"`
}

func TestGetMappedCols_NestedVO_MultiLevel(t *testing.T) {
	m := &rootLevel{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2) // id + mid.deep.value
}

type level4 struct {
	Value string `db:"value"`
}

type level3 struct {
	L4 level4 `db:"l4"`
}

type level2 struct {
	L3 level3 `db:"l3"`
}

type level1 struct {
	L2 level2 `db:"l2"`
}

type fourLevelRoot struct {
	ID int64  `db:"id"`
	L1 level1 `db:"l1"`
}

func TestGetMappedCols_FourLevelsDeep_StructureAndScan(t *testing.T) {
	m := &fourLevelRoot{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2) // id + l1.l2.l3.l4.value

	rows := openWithRows(t, &fakeRows{
		cols: []string{"id", "value"},
		rows: [][]driver.Value{{int64(42), "deep-hello"}},
	})
	defer rows.Close()

	list, err := GetContentList[fourLevelRoot](rows)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(42), list[0].ID)
	assert.Equal(t, "deep-hello", list[0].L1.L2.L3.L4.Value)
}

type modelWithTime struct {
	ID        int64     `db:"id"`
	CreatedAt time.Time `db:"created_at"`
}

func TestGetMappedCols_TimeIsPrimitive_NotRecursed(t *testing.T) {
	m := &modelWithTime{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2) // id + created_at (time.Time is primitive)
}

type customScanner struct {
	Raw string
}

func (c *customScanner) Scan(src any) error {
	if s, ok := src.(string); ok {
		c.Raw = s
	}
	return nil
}

type modelWithScanner struct {
	ID   int64         `db:"id"`
	Data customScanner `db:"data"`
}

func TestGetMappedCols_SqlScannerIsPrimitive_NotRecursed(t *testing.T) {
	m := &modelWithScanner{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 2) // id + data — customScanner is treated as primitive
}

type nestedWithoutOuterTag struct {
	ID    int64     `db:"id"`
	Inner pricingVO // no db tag → entire VO ignored
}

func TestGetMappedCols_NestedWithoutOuterTag_Ignored(t *testing.T) {
	m := &nestedWithoutOuterTag{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 1) // only id
}

type emptyInnerStruct struct {
	Price float64 // no db tag
}

type modelWithEmptyInner struct {
	ID    int64            `db:"id"`
	Inner emptyInnerStruct `db:"inner"`
}

func TestGetMappedCols_NestedInnerAllUntagged_Empty(t *testing.T) {
	m := &modelWithEmptyInner{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 1) // only id — inner has no tagged leaves
}

type modelWithSliceAndVO struct {
	ID    int64     `db:"id"`
	Tags  []string  `db:"tags"`
	Price pricingVO `db:"price"`
}

func TestGetMappedCols_MixedSliceAndVO(t *testing.T) {
	m := &modelWithSliceAndVO{}
	cols := GetMappedCols(m)
	require.Len(t, cols, 3) // id + tags (pq.Array) + price.price
}

// Plan cache

func TestTypePlan_CachedAcrossCalls(t *testing.T) {
	tp := reflect.TypeOf(productWithVO{})
	p1 := getTypePlan(tp)
	p2 := getTypePlan(tp)
	assert.Same(t, p1, p2, "second call should reuse cached plan pointer")
}

func TestTypePlan_ConcurrentSafe(t *testing.T) {
	type concurrentModel struct {
		A int `db:"a"`
		B int `db:"b"`
	}
	tp := reflect.TypeOf(concurrentModel{})

	const workers = 32
	seen := make([]*typePlan, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			seen[idx] = getTypePlan(tp)
		}(i)
	}
	wg.Wait()

	for i := 1; i < workers; i++ {
		assert.Same(t, seen[0], seen[i], "all goroutines should see the same plan")
	}
}

func TestTypePlan_SimpleType(t *testing.T) {
	p := getTypePlan(reflect.TypeOf(42))
	assert.True(t, p.simple)
	assert.Empty(t, p.fields)
}

// End-to-end: verify values land in the correct nested fields after scan

func TestGetContentList_NestedVO_ValuesLandCorrectly(t *testing.T) {
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id", "name", "price"},
		rows: [][]driver.Value{{int64(7), "Widget", float64(9.99)}},
	})
	defer rows.Close()

	list, err := GetContentList[productWithVO](rows)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, int64(7), list[0].ID)
	assert.Equal(t, "Widget", list[0].Name)
	assert.Equal(t, 9.99, list[0].Price.Price)
}

func TestGetContentList_NestedVO_MultipleRows(t *testing.T) {
	rows := openWithRows(t, &fakeRows{
		cols: []string{"id", "name", "price"},
		rows: [][]driver.Value{
			{int64(1), "A", float64(1.5)},
			{int64(2), "B", float64(2.5)},
			{int64(3), "C", float64(3.5)},
		},
	})
	defer rows.Close()

	list, err := GetContentList[productWithVO](rows)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, 1.5, list[0].Price.Price)
	assert.Equal(t, 2.5, list[1].Price.Price)
	assert.Equal(t, 3.5, list[2].Price.Price)
}

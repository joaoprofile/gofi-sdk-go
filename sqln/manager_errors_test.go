package sqln

import (
	"context"
	"database/sql"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closedDB returns a *sql.DB that has already been closed, causing any query to fail.
func closedDB(t *testing.T) *sql.DB {
	t.Helper()
	initDriver()
	db, err := sql.Open(testDriverName, "ok")
	if err != nil {
		t.Fatalf("closedDB open: %v", err)
	}
	_ = db.Close() // close immediately so all subsequent calls fail
	return db
}

// fetchList — FetchRows returns an error (closed DB)
func TestFetchList_FetchRowsError_ReturnsError(t *testing.T) {
	db := closedDB(t)
	m := &manager[scalarItem]{
		ctx:   context.Background(),
		query: "SELECT id FROM t",
	}
	list, err := m.fetchList(db)
	assert.Nil(t, list)
	assert.Error(t, err)
}

// fetchUniqueResult — scan error that is NOT sql.ErrNoRows
// mappedItem has 2 gofi-tagged fields → GetMappedCols returns 2 destinations.
// count-rows DSN returns 1 column → Scan("expected 2 destination args, not 1").
func TestFetchUniqueResult_ScanError_NotErrNoRows_ReturnsError(t *testing.T) {
	db := openDB(t, "count-rows") // 1 col, but mappedItem needs 2
	m := &manager[mappedItem]{
		ctx:   context.Background(),
		query: "SELECT count FROM t",
	}
	result, err := m.fetchUniqueResult(db)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.NotEqual(t, sql.ErrNoRows, err)
}

// fetchPagedList — FetchRows returns an error (closed DB)
func TestFetchPagedList_FetchRowsError_ReturnsError(t *testing.T) {
	setupGlobal(t, "ok")
	db := closedDB(t)
	pr := pagination.NewPageRequest(0, 10, nil)
	m := &manager[scalarItem]{
		ctx:   context.Background(),
		query: "SELECT id FROM t",
		page:  pr,
	}
	list, err := m.fetchPagedList(db)
	assert.Nil(t, list)
	assert.Error(t, err)
}

// ExecutePagedQuery — pageTotal scan fails → error propagated
// Using a closed DB makes both pageTotal and fetchPagedList fail.
// ExecutePagedQuery checks err after pageTotal; the error is returned before fetchPagedList.
func TestExecutePagedQuery_PageTotalError_ReturnsError(t *testing.T) {
	setupGlobal(t, "ok") // dialect available
	db := closedDB(t)
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)

	result, err := m.ExecutePagedQuery(db)
	assert.Nil(t, result)
	assert.Error(t, err)
}

// fetchUniqueResult — success path with cache set
func TestFetchUniqueResult_NoRows_CacheNil_ReturnsNilNoError(t *testing.T) {
	db := openDB(t, "no-rows")
	m := &manager[mappedItem]{
		ctx:   context.Background(),
		query: "SELECT id, name FROM t",
	}
	result, err := m.fetchUniqueResult(db)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// fetchUniqueResult — successful scan returns the model
// struct-rows DSN → 2 cols (id int64, name string) → mappedItem scans cleanly.
func TestFetchUniqueResult_SuccessfulScan_ReturnsModel(t *testing.T) {
	db := openDB(t, "struct-rows")
	m := &manager[mappedItem]{
		ctx:   context.Background(),
		query: "SELECT id, name FROM t",
	}
	result, err := m.fetchUniqueResult(db)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(1), result.ID)
}

func TestFetchUniqueResult_SuccessfulScan_WithCache_SetsCache(t *testing.T) {
	db := openDB(t, "struct-rows")
	c := newTestCache[mappedItem]()
	m := &manager[mappedItem]{
		ctx:   context.Background(),
		query: "SELECT id, name FROM t",
		cache: c,
	}
	// cache.Set fails silently (no Redis) — result is still returned
	result, err := m.fetchUniqueResult(db)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// fetchList — mapping error: count-rows DSN returns 1 col but mappedItem needs 2
func TestFetchList_MappingError_ReturnsError(t *testing.T) {
	db := openDB(t, "count-rows") // 1 col, mappedItem has 2 gofi-tagged fields
	m := &manager[mappedItem]{
		ctx:   context.Background(),
		query: "SELECT id, name FROM t",
	}
	list, err := m.fetchList(db)
	assert.Nil(t, list)
	assert.Error(t, err)
}

// resolveFromCriteria — with page set, uses BuildBase instead of Build
func TestResolveFromCriteria_WithPage_UsesBuildBase(t *testing.T) {
	setupGlobal(t, "count-rows")
	q := CriteriaFrom("products", "p")
	pr := NewPageRequest(0, 10, nil)

	// PagedList exercises resolveFromCriteria(conn) with page != nil → BuildBase path
	m := FindFromCriteria[scalarItem](context.Background(), q).WithPage(pr)
	result, err := m.PagedList()

	require.NoError(t, err)
	assert.NotNil(t, result)
}

// A request-supplied criteria field that is not a column fails every execution
// path before any SQL reaches the database.
func TestFindFromCriteria_InvalidFieldFailsWithoutQuerying(t *testing.T) {
	setupGlobal(t, "struct-rows")
	resetRecordedQueries(t)
	ctx := context.Background()
	payload := "id; DROP TABLE users"
	bad := func() *CriteriaQuery { return CriteriaFrom("t", "").Where(criteria.Eq(payload, 1)) }

	_, err := FindFromCriteria[mappedItem](ctx, bad()).List()
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
	_, err = FindFromCriteria[mappedItem](ctx, bad()).UniqueResult()
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
	_, err = FindFromCriteria[mappedItem](ctx, bad()).WithPage(NewPageRequest(0, 10, nil)).PagedList()
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
	_, err = FindFromCriteria[mappedItem](ctx, CriteriaFrom("t", "").OrderBy(Asc(payload))).List()
	assert.ErrorIs(t, err, criteria.ErrInvalidField)
	for _, err := range FindFromCriteria[mappedItem](ctx, bad()).All() {
		assert.ErrorIs(t, err, criteria.ErrInvalidField)
	}
	assert.Empty(t, recorded(t), "no query may reach the database")
}

// Re-export smoke tests — one call each confirms the wrapper is wired
func TestReExports_Statement(t *testing.T) {
	setupGlobal(t, "ok")
	s := NewStatement()
	assert.NotNil(t, s)
}

func TestReExports_Transaction(t *testing.T) {
	tx := NewTransaction()
	assert.NotNil(t, tx)
	tx2 := NewTransaction(sql.LevelSerializable)
	assert.NotNil(t, tx2)
}

func TestReExports_Pagination(t *testing.T) {
	s := NewSort("id", ASC)
	assert.Equal(t, "id", s.Field)

	pr := NewPageRequest(1, 10, []Sort{s})
	assert.Equal(t, uint16(1), pr.Page)

	pr2, err := NewPageRequestFilter(nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, pr2)
}

func TestReExports_Filter(t *testing.T) {
	f := NewFilter("name", Eq, "Emilia")
	assert.NotNil(t, f)

	and := AND()
	assert.NotNil(t, and)

	or := OR()
	assert.NotNil(t, or)

	fs := NewFilters()
	assert.NotNil(t, fs)

	// A nil dialect resolves from the active global connection.
	setupGlobal(t, "ok")
	qp, err := BuildQuery("SELECT 1", nil, fs, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, qp)
}

func TestReExports_BuildQuery(t *testing.T) {
	setupGlobal(t, "ok")
	fs := NewFilters().Add(NewFilter("status", Eq, "active"))
	qp, err := BuildQuery("SELECT 1 WHERE 1=1", nil, fs, AllowColumns("status"), fakeDialect{})
	require.NoError(t, err)
	assert.Contains(t, qp.Query, "status")

	_, err = BuildQuery("SELECT 1 WHERE 1=1", nil, fs, FilterMapping{}, fakeDialect{})
	assert.ErrorIs(t, err, ErrInvalidFilter)
}

func TestReExports_Cache(t *testing.T) {
	// NewCache re-export
	c := NewCache[string]("test-export", 0)
	assert.NotNil(t, c)

	// NewCacheRedis re-export — no Redis available; logs error but does not panic
	assert.NotPanics(t, func() { NewCacheRedis() })

	// InstanceRedis re-export — triggers NewCacheRedis internally, returns client (may be nil)
	assert.NotPanics(t, func() { InstanceRedis() })
}

func TestReExports_BuildClause(t *testing.T) {
	predicates := []Predicate{criteria.Eq("id", 1)}
	clause, params := built(t)(BuildClause(predicates, fakeDialect{}))
	assert.NotEmpty(t, clause)
	assert.Len(t, params, 1)
}

func TestReExports_Asc_Desc(t *testing.T) {
	asc := Asc("name")
	assert.Equal(t, "name", asc.Field)

	desc := Desc("created_at")
	assert.Equal(t, "created_at", desc.Field)
}

func TestNewPageRequestFilter_WithNonNilParams(t *testing.T) {
	fs := NewFilters()
	fs.Params.Page = 2
	fs.Params.Limit = 20
	fs.Params.SortField = "name"
	fs.Params.SortDirection = "DESC"

	m := FilterMapping{"name": {Column: "c.full_name", Sortable: true}}
	pr, err := NewPageRequestFilter(fs, m)
	require.NoError(t, err)
	assert.Equal(t, uint16(2), pr.Page)
	assert.Equal(t, uint16(20), pr.Limit)
	assert.Equal(t, "c.full_name DESC", pr.GetOrder(), "sort field resolved through the mapping")

	fs.Params.SortField = "password_hash"
	_, err = NewPageRequestFilter(fs, m)
	assert.ErrorIs(t, err, ErrInvalidFilter, "unmapped sort fields are rejected")
}

// List / Execute / UniqueResult — nil conn branch is dead code
// (connection() always panics before returning nil)
// We verify the live path (global set) returns without panic.
func TestList_WithGlobal_ReturnsResult(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[scalarItem](context.Background(), "SELECT id FROM t")
	list, err := m.List()
	require.NoError(t, err)
	assert.NotNil(t, list)
}

func TestExecute_WithGlobal_ReturnsResult(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	_, err := m.Execute()
	require.NoError(t, err)
}

func TestUniqueResult_WithGlobal_ReturnsResult(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	_, err := m.UniqueResult()
	require.NoError(t, err)
}

// ExecuteListQuery — cache result != nil path would need real Redis.
// Verify cache-nil path sets cache (cache.Set is called; since Redis is absent
// the error is silently ignored by fetchList).
func TestFetchList_WithCacheSet_DoesNotPanic(t *testing.T) {
	// cache.Set is called when list is fetched and cache != nil.
	// With no Redis, Set returns an error that is ignored by fetchList.
	setupGlobal(t, "no-rows")
	db := openDB(t, "no-rows")
	c := newTestCache[scalarItem]()
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithCache(c)

	assert.NotPanics(t, func() {
		_, _ = m.ExecuteListQuery(db)
	})
}

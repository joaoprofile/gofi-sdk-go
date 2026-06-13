package sqln

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/sqln/cache"
	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/filter"
	"github.com/joaoprofile/gofi/sqln/pagination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Constructors
func TestFind_SetsQueryAndArgs(t *testing.T) {
	m := Find[scalarItem](context.Background(), "SELECT 1", 42)
	assert.Equal(t, "SELECT 1", m.query)
	assert.Equal(t, []any{42}, m.args)
	assert.Nil(t, m.page)
	assert.Nil(t, m.cache)
}

func TestFind_NoArgs(t *testing.T) {
	m := Find[scalarItem](context.Background(), "SELECT 1")
	assert.Equal(t, "SELECT 1", m.query)
	assert.Empty(t, m.args)
}

func TestNewCustomQuery_SameAsFindBehavior(t *testing.T) {
	m := NewCustomQuery[scalarItem](context.Background(), "SELECT 2", "x")
	assert.Equal(t, "SELECT 2", m.query)
	assert.Equal(t, []any{"x"}, m.args)
}

func TestFindWithFilter_SetsQueryFromQueryParam(t *testing.T) {
	qp := &filter.QueryParam{Query: "SELECT * FROM t WHERE id = ?", Params: []any{1}}
	m := FindWithFilter[scalarItem](context.Background(), qp)
	assert.Equal(t, qp.Query, m.query)
	assert.Equal(t, qp.Params, m.args)
}

// Builder methods
func TestWithPage_SetsPage(t *testing.T) {
	pr := pagination.NewPageRequest(1, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT 1").WithPage(pr)
	assert.Equal(t, pr, m.page)
}

func TestWithPage_Chaining_ReturnsSameManager(t *testing.T) {
	pr := pagination.NewPageRequest(0, 15, nil)
	m := Find[scalarItem](context.Background(), "SELECT 1")
	result := m.WithPage(pr)
	assert.Same(t, m, result)
}

func TestWithCache_SetsCache(t *testing.T) {
	c := cache.NewCache[scalarItem]("test", time.Minute)
	m := Find[scalarItem](context.Background(), "SELECT 1").WithCache(c)
	assert.Equal(t, c, m.cache)
}

func TestWithCache_Chaining_ReturnsSameManager(t *testing.T) {
	c := cache.NewCache[scalarItem]("test", time.Minute)
	m := Find[scalarItem](context.Background(), "SELECT 1")
	result := m.WithCache(c)
	assert.Same(t, m, result)
}

// ExecuteListQuery
func TestExecuteListQuery_NilDB_ReturnsError(t *testing.T) {
	m := Find[scalarItem](context.Background(), "SELECT 1")
	list, err := m.ExecuteListQuery(nil)
	assert.Nil(t, list)
	assert.ErrorContains(t, err, ErrDatabaseNotInitialized)
}

func TestExecuteListQuery_EmptyQuery_ReturnsError(t *testing.T) {
	db := openDB(t, "ok")
	m := Find[scalarItem](context.Background(), "")
	list, err := m.ExecuteListQuery(db)
	assert.Nil(t, list)
	assert.ErrorContains(t, err, ErrMsgQueryIsEmpty)
}

func TestExecuteListQuery_Success_EmptyResult(t *testing.T) {
	db := openDB(t, "no-rows")
	m := Find[scalarItem](context.Background(), "SELECT id FROM t")
	list, err := m.ExecuteListQuery(db)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestExecuteListQuery_WithCache_CacheMiss_FetchesFromDB(t *testing.T) {
	// Cache without Redis → validate() returns error → treated as miss → falls through
	db := openDB(t, "no-rows")
	c := cache.NewCache[scalarItem]("mykey", time.Minute)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithCache(c)
	list, err := m.ExecuteListQuery(db)
	require.NoError(t, err)
	assert.Empty(t, list) // DB returned no rows
}

// ExecuteUniqueResultQuery
func TestExecuteUniqueResultQuery_NilDB_ReturnsError(t *testing.T) {
	m := Find[mappedItem](context.Background(), "SELECT 1")
	result, err := m.ExecuteUniqueResultQuery(nil)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, ErrDatabaseNotInitialized)
}

func TestExecuteUniqueResultQuery_EmptyQuery_ReturnsError(t *testing.T) {
	db := openDB(t, "ok")
	m := Find[mappedItem](context.Background(), "")
	result, err := m.ExecuteUniqueResultQuery(db)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, ErrMsgQueryIsEmpty)
}

func TestExecuteUniqueResultQuery_NoRows_ReturnsNil(t *testing.T) {
	// no-rows DSN → driver returns EOF immediately → sql.ErrNoRows
	db := openDB(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t WHERE id = 99")
	result, err := m.ExecuteUniqueResultQuery(db)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestExecuteUniqueResultQuery_WithCache_CacheMiss_FetchesFromDB(t *testing.T) {
	db := openDB(t, "no-rows")
	c := cache.NewCache[mappedItem]("uq", time.Minute)
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithCache(c)
	result, err := m.ExecuteUniqueResultQuery(db)
	require.NoError(t, err)
	assert.Nil(t, result) // no rows in DB
}

// ExecutePagedQuery
func TestExecutePagedQuery_NilPage_ReturnsError(t *testing.T) {
	db := openDB(t, "ok")
	m := Find[scalarItem](context.Background(), "SELECT 1")
	// page is nil by default
	result, err := m.ExecutePagedQuery(db)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, ErrPageIsEmpty)
}

func TestExecutePagedQuery_NilDB_ReturnsError(t *testing.T) {
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT 1").WithPage(pr)
	result, err := m.ExecutePagedQuery(nil)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, ErrDatabaseNotInitialized)
}

func TestExecutePagedQuery_EmptyQuery_ReturnsError(t *testing.T) {
	db := openDB(t, "ok")
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "").WithPage(pr)
	result, err := m.ExecutePagedQuery(db)
	assert.Nil(t, result)
	assert.ErrorContains(t, err, ErrMsgQueryIsEmpty)
}

func TestExecutePagedQuery_Success(t *testing.T) {
	// count-rows DSN returns int64(5) on every query (used for both COUNT and list)
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")

	pr := pagination.NewPageRequest(0, 10, []Sort{NewSort("id", ASC)})
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)

	result, err := m.ExecutePagedQuery(db)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, uint64(10), result.Size)
	assert.Equal(t, uint64(0), result.Number)
}

func TestExecutePagedQuery_WithCache_CacheMiss_FetchesFromDB(t *testing.T) {
	// Cache without Redis → Get returns error → treated as miss → falls through to DB.
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")

	c := cache.NewCache[scalarItem]("paged-key", time.Minute)
	pr := pagination.NewPageRequest(0, 10, []Sort{NewSort("id", ASC)})
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr).WithCache(c)

	result, err := m.ExecutePagedQuery(db)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, uint64(10), result.Size)
}

// Execute / List / UniqueResult via global connection
func TestList_GlobalConnection_Success(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[scalarItem](context.Background(), "SELECT id FROM t")
	list, err := m.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestExecute_GlobalConnection_NoRows_ReturnsNil(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	result, err := m.Execute()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestUniqueResult_GlobalConnection_NoRows_ReturnsNil(t *testing.T) {
	setupGlobal(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	result, err := m.UniqueResult()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestPagedList_GlobalConnection_Success(t *testing.T) {
	setupGlobal(t, "count-rows")
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)
	result, err := m.PagedList()
	require.NoError(t, err)
	require.NotNil(t, result)
}

// connection() panics when global not set
func TestConnection_PanicsWhenGlobalNotSet(t *testing.T) {
	connection.ResetGlobalForTest()
	m := Find[scalarItem](context.Background(), "SELECT 1")
	assert.Panics(t, func() {
		m.connection()
	})
}

// validate

func TestValidate_NilDB_ReturnsError(t *testing.T) {
	m := Find[scalarItem](context.Background(), "SELECT 1")
	err := m.validate(nil)
	assert.ErrorContains(t, err, ErrDatabaseNotInitialized)
}

func TestValidate_EmptyQuery_ReturnsError(t *testing.T) {
	db := openDB(t, "ok")
	m := Find[scalarItem](context.Background(), "")
	err := m.validate(db)
	assert.ErrorContains(t, err, ErrMsgQueryIsEmpty)
}

func TestValidate_ValidArgs_NoError(t *testing.T) {
	db := openDB(t, "ok")
	m := Find[scalarItem](context.Background(), "SELECT 1")
	err := m.validate(db)
	assert.NoError(t, err)
}

// fetchList — internal path, exercise via ExecuteListQuery
func TestFetchList_WithArgs(t *testing.T) {
	db := openDB(t, "no-rows")
	m := Find[scalarItem](context.Background(), "SELECT id FROM t WHERE id = ?", 1)
	list, err := m.ExecuteListQuery(db)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestFetchList_InTransactionContext(t *testing.T) {
	db := openDB(t, "no-rows")
	ctx := txContext(t, db)
	m := &manager[scalarItem]{ctx: ctx, query: "SELECT id FROM t", args: nil}
	list, err := m.ExecuteListQuery(db)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// fetchUniqueResult — via ExecuteUniqueResultQuery with scan
func TestFetchUniqueResult_InTransactionContext(t *testing.T) {
	db := openDB(t, "no-rows")
	ctx := txContext(t, db)
	m := &manager[mappedItem]{ctx: ctx, query: "SELECT id, name FROM t", args: nil}
	result, err := m.ExecuteUniqueResultQuery(db)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// Page calculation edge cases
func TestExecutePagedQuery_TotalPagesCalculation(t *testing.T) {
	// count-rows returns 5; limit=10 → ceil(5/10) = 1 page
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)

	result, err := m.ExecutePagedQuery(db)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), result.TotalElements)
	assert.Equal(t, uint64(1), result.TotalPages) // ceil(5/10)
}

func TestExecutePagedQuery_PageNumber(t *testing.T) {
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	pr := pagination.NewPageRequest(2, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)

	result, err := m.ExecutePagedQuery(db)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), result.Number)
}

func TestExecutePagedQuery_WithSortOrder(t *testing.T) {
	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	pr := pagination.NewPageRequest(0, 5, []Sort{
		NewSort("name", ASC),
		NewSort("id", DESC),
	})
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithPage(pr)

	result, err := m.ExecutePagedQuery(db)
	require.NoError(t, err)
	require.NotNil(t, result)
}

// Cache path — falls through when Redis not available
func TestExecuteListQuery_WithCache_AlwaysFallsThrough_WhenRedisUnavailable(t *testing.T) {
	db := openDB(t, "no-rows")
	c := cache.NewCache[scalarItem]("list-key", time.Minute)
	m := Find[scalarItem](context.Background(), "SELECT id FROM t").WithCache(c)

	// cache.List → error (redis nil) → condition (err != nil) → fetchList
	list, err := m.ExecuteListQuery(db)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestExecuteUniqueResultQuery_WithCache_FallsThrough_WhenRedisUnavailable(t *testing.T) {
	db := openDB(t, "no-rows")
	c := cache.NewCache[mappedItem]("uq-key", time.Minute)
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithCache(c)

	result, err := m.ExecuteUniqueResultQuery(db)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// Type safety — ensure generic works with different types
func TestFind_WithStringType(t *testing.T) {
	m := Find[string](context.Background(), "SELECT name FROM t")
	assert.Equal(t, "SELECT name FROM t", m.query)
}

func TestFind_WithStructType(t *testing.T) {
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	assert.NotNil(t, m)
}

// sql.ErrNoRows vs other scan errors
func TestExecuteUniqueResultQuery_ErrNoRows_ReturnsNilNotError(t *testing.T) {
	db := openDB(t, "no-rows")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t WHERE false")
	result, err := m.ExecuteUniqueResultQuery(db)
	// ErrNoRows must be swallowed — return (nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// Global Execute/List panic when DB not initialized
func TestList_PanicsWhenGlobalNotSet(t *testing.T) {
	connection.ResetGlobalForTest()
	m := Find[scalarItem](context.Background(), "SELECT 1")
	assert.Panics(t, func() { _, _ = m.List() })
}

func TestExecute_PanicsWhenGlobalNotSet(t *testing.T) {
	connection.ResetGlobalForTest()
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	assert.Panics(t, func() { _, _ = m.Execute() })
}

func TestUniqueResult_PanicsWhenGlobalNotSet(t *testing.T) {
	connection.ResetGlobalForTest()
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	assert.Panics(t, func() { _, _ = m.UniqueResult() })
}

func TestPagedList_PanicsWhenGlobalNotSet(t *testing.T) {
	connection.ResetGlobalForTest()
	pr := pagination.NewPageRequest(0, 10, nil)
	m := Find[scalarItem](context.Background(), "SELECT 1").WithPage(pr)
	assert.Panics(t, func() { _, _ = m.PagedList() })
}

// Unused — suppress compiler warning for unused sql import
var _ = sql.ErrNoRows

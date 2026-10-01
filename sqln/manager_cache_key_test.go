package sqln

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/sqln/cache"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPagedQuery_CacheIsScopedPerPage(t *testing.T) {
	mr := miniredis.RunT(t)
	cache.UseClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	t.Cleanup(func() { cache.UseClient(nil) })

	setupGlobal(t, "count-rows")
	db := openDB(t, "count-rows")
	c := cache.NewCache[scalarItem]("paged-scope", time.Minute)
	ctx := context.Background()

	for _, page := range []uint16{0, 1, 0} {
		pr := pagination.NewPageRequest(page, 10, []Sort{NewSort("id", ASC)})
		res, err := Find[scalarItem](ctx, "SELECT id FROM t").WithPage(pr).WithCache(c).ExecutePagedQuery(db)
		require.NoError(t, err)
		assert.Equal(t, uint64(page), res.Number, "page %d served another page from cache", page)
	}
}

func TestListQuery_CacheIsScopedPerArgs(t *testing.T) {
	db := setupGlobal(t, "ok")
	key := func(args ...any) string {
		return mustKey(t, Find[scalarItem](context.Background(), "SELECT id FROM t WHERE x = $1", args...), db, "list")
	}
	assert.NotEqual(t, key(1), key(2))
	assert.NotEqual(t, key(1), key("1"), "int 1 and string \"1\" must not collide")
	assert.Equal(t, key(1), key(1))
	assert.NotEqual(t, key("a", "bc"), key("ab", "c"), "argument boundaries are part of the key")
}

// masked hides its value from fmt, as secret or ID types often do.
type masked string

func (masked) String() string { return "***" }

type valuer struct{ v string }

func (v valuer) Value() (driver.Value, error) { return v.v, nil }

func TestCacheKey_EncodesDriverValueNotFmt(t *testing.T) {
	db := setupGlobal(t, "ok")
	key := func(a any) string {
		return mustKey(t, Find[scalarItem](context.Background(), "SELECT 1 WHERE x = $1", a), db, "list")
	}
	assert.NotEqual(t, key(masked("tenant-a")), key(masked("tenant-b")), "fmt-masked values must not collide")
	assert.NotEqual(t, key(valuer{"a"}), key(valuer{"b"}))
	assert.NotEqual(t, key([]string{"a", "b"}), key([]string{"a,b"}))

	type opaque struct{ v string }
	_, ok := Find[scalarItem](context.Background(), "SELECT 1 WHERE x = $1", opaque{"a"}).WithCache(newTestCache[scalarItem]()).cacheKey(db, "list")
	assert.False(t, ok, "an argument without a deterministic encoding disables the cache")
}

func TestCacheKey_ScopedPerConnection(t *testing.T) {
	a := newConn(t, "struct-rows")
	b := newConn(t, "multi-rows")
	a2 := newConn(t, "struct-rows")
	key := func(c *connection.Connection) string {
		return mustKey(t, Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithConnection(c), c.DB(), "list")
	}
	assert.NotEqual(t, key(a), key(b), "two databases must not share cache entries")
	assert.Equal(t, key(a), key(a2), "the same database keeps one key across pools and processes")

	// A pool outside any connection gets a process-local identity.
	x, y := openDB(t, "ok"), openDB(t, "ok")
	m := Find[mappedItem](context.Background(), "SELECT id, name FROM t")
	assert.NotEqual(t, mustKey(t, m, x, "list"), mustKey(t, m, y, "list"))
	assert.Equal(t, mustKey(t, m, x, "list"), mustKey(t, m, x, "list"))
}

func TestCacheKey_ScopedPerCacheScope(t *testing.T) {
	db := setupGlobal(t, "ok")
	ctx := context.Background()
	m := func(ctx context.Context) *manager[scalarItem] { return Find[scalarItem](ctx, "SELECT id FROM t") }
	assert.NotEqual(t, mustKey(t, m(ctx).WithCacheScope("t1"), db, "list"), mustKey(t, m(ctx).WithCacheScope("t2"), db, "list"))
	assert.NotEqual(t, mustKey(t, m(ContextWithCacheScope(ctx, "t1")), db, "list"), mustKey(t, m(ContextWithCacheScope(ctx, "t2")), db, "list"))
	assert.NotEqual(t, mustKey(t, m(ctx), db, "list"), mustKey(t, m(ctx), db, "unique"), "list and unique results have different shapes")
}

// Tenant B, on its own database, must never be served tenant A's cached rows.
func TestCachedQuery_TenantDatabasesDoNotShareEntries(t *testing.T) {
	c := withMiniredisCache[mappedItem](t, "tenants")
	a := newConn(t, "struct-rows")
	b := newConn(t, "multi-rows")
	ctx := context.Background()

	got, err := Find[mappedItem](ctx, "SELECT id, name FROM t").WithConnection(a).WithCache(c).List()
	require.NoError(t, err)
	require.Len(t, got, 1)
	got, err = Find[mappedItem](ctx, "SELECT id, name FROM t").WithConnection(b).WithCache(c).List()
	require.NoError(t, err)
	assert.Len(t, got, 3, "tenant B received tenant A's cached result")
}

// Rows read inside a transaction may be uncommitted: never cached, never served from cache.
func TestCachedQuery_TransactionBypassesCache(t *testing.T) {
	mr := miniredis.RunT(t)
	c := cache.NewCache[mappedItem]("tx", time.Minute).WithClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	db := setupGlobal(t, "struct-rows")
	tx, err := db.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	txCtx := connection.WithTx(context.Background(), db, tx)

	_, ok := Find[mappedItem](txCtx, "SELECT id, name FROM t").WithCache(c).cacheKey(db, "list")
	assert.False(t, ok)

	_, err = Find[mappedItem](txCtx, "SELECT id, name FROM t").WithCache(c).List()
	require.NoError(t, err)
	assert.Empty(t, mr.Keys(), "a result read in a transaction was written to the cache")

	// An entry cached outside the transaction is not served inside it.
	key := mustKey(t, Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithCache(c), db, "list")
	require.NoError(t, c.SetKeyed(context.Background(), key, []mappedItem{{ID: 99, Name: "stale"}}))
	got, err := Find[mappedItem](txCtx, "SELECT id, name FROM t").WithCache(c).List()
	require.NoError(t, err)
	assert.Equal(t, []mappedItem{{ID: 1, Name: "Emilia"}}, got)
}

func mustKey[T any](t *testing.T, m *manager[T], db *sql.DB, kind string) string {
	t.Helper()
	if m.cache == nil {
		m.WithCache(newTestCache[T]())
	}
	k, ok := m.cacheKey(db, kind)
	require.True(t, ok, "query must be cacheable")
	return k
}

func newConn(t *testing.T, dsn string) *connection.Connection {
	t.Helper()
	initDriver()
	c, err := connection.NewConnection(connection.Config{Driver: connection.DriverName(testDriverName), DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func withMiniredisCache[T any](t *testing.T, name string) *cache.Cache[T] {
	t.Helper()
	mr := miniredis.RunT(t)
	return cache.NewCache[T](name, time.Minute).WithClient(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
}

package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi/base/session"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupRedis(t *testing.T) (session.Driver, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		client.Close()
		mr.Close()
	})

	return session.NewRedisDriver(client, 10*time.Minute), mr
}

func setupRedisSession(t *testing.T) (*session.Session, session.Driver, *miniredis.Miniredis) {
	t.Helper()
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)

	driver, mr := setupRedis(t)
	s := session.New(driver, &session.Config{Prefix: "test", TTL: 10 * time.Minute})
	return s, driver, mr
}

func newEntry(ttl time.Duration) *session.Entry {
	now := time.Now()
	return &session.Entry{
		ID:        "test-id",
		Data:      session.SessionData{"k": "v"},
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

// ---------------------------------------------------------------------------
// Driver: Save / Get
// ---------------------------------------------------------------------------

func TestRedisDriver_SaveAndGet(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	e := newEntry(10 * time.Minute)
	require.NoError(t, driver.Save(ctx, "key1", e))

	got, err := driver.Get(ctx, "key1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Data, got.Data)
}

func TestRedisDriver_Get_MissingKey(t *testing.T) {
	driver, _ := setupRedis(t)
	got, err := driver.Get(context.Background(), "missing")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestRedisDriver_Save_UsesEntryTTL(t *testing.T) {
	driver, mr := setupRedis(t)
	e := newEntry(5 * time.Minute)
	require.NoError(t, driver.Save(context.Background(), "key-ttl", e))

	ttl := mr.TTL("key-ttl")
	assert.True(t, ttl > 0 && ttl <= 5*time.Minute)
}

func TestRedisDriver_Save_FallsBackToDriverTTL_WhenExpired(t *testing.T) {
	driver, mr := setupRedis(t)
	e := newEntry(-time.Second) // already expired
	require.NoError(t, driver.Save(context.Background(), "key-exp", e))

	ttl := mr.TTL("key-exp")
	assert.True(t, ttl > 0, "should use driver TTL as fallback")
}

// ---------------------------------------------------------------------------
// Driver: Delete
// ---------------------------------------------------------------------------

func TestRedisDriver_Delete(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	require.NoError(t, driver.Save(ctx, "del-key", newEntry(time.Minute)))
	require.NoError(t, driver.Delete(ctx, "del-key"))

	got, err := driver.Get(ctx, "del-key")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestRedisDriver_Delete_NonExistent(t *testing.T) {
	driver, _ := setupRedis(t)
	assert.NoError(t, driver.Delete(context.Background(), "ghost"))
}

// ---------------------------------------------------------------------------
// Driver: ScanAll
// ---------------------------------------------------------------------------

func TestRedisDriver_ScanAll_ReturnsMatchingKeys(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	e := newEntry(time.Minute)
	require.NoError(t, driver.Save(ctx, "ns:a", e))
	require.NoError(t, driver.Save(ctx, "ns:b", e))
	require.NoError(t, driver.Save(ctx, "other:c", e))

	keys, err := driver.ScanAll(ctx, "ns:")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"ns:a", "ns:b"}, keys)
}

func TestRedisDriver_ScanAll_NoMatches(t *testing.T) {
	driver, _ := setupRedis(t)
	keys, err := driver.ScanAll(context.Background(), "nothing:")
	assert.NoError(t, err)
	assert.Empty(t, keys)
}

// ---------------------------------------------------------------------------
// Driver: CleanExpired
// ---------------------------------------------------------------------------

func TestRedisDriver_CleanExpired(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	require.NoError(t, driver.Save(ctx, "ns:valid", newEntry(10*time.Minute)))
	require.NoError(t, driver.Save(ctx, "ns:expired", newEntry(-time.Second)))

	require.NoError(t, driver.CleanExpired(ctx, "ns:"))

	got, err := driver.Get(ctx, "ns:valid")
	require.NoError(t, err)
	assert.NotNil(t, got)

	got, err = driver.Get(ctx, "ns:expired")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// Driver: AcquireLock / ReleaseLock / IsLocked
// ---------------------------------------------------------------------------

func TestRedisDriver_AcquireLock_Success(t *testing.T) {
	driver, _ := setupRedis(t)
	ok, err := driver.AcquireLock(context.Background(), "lk1", time.Second)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRedisDriver_AcquireLock_AlreadyLocked(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	ok, _ := driver.AcquireLock(ctx, "lk2", time.Second)
	require.True(t, ok)

	ok, err := driver.AcquireLock(ctx, "lk2", time.Second)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRedisDriver_ReleaseLock_AllowsReacquire(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	_, _ = driver.AcquireLock(ctx, "lk3", time.Second)
	require.NoError(t, driver.ReleaseLock(ctx, "lk3"))

	ok, err := driver.AcquireLock(ctx, "lk3", time.Second)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestRedisDriver_IsLocked(t *testing.T) {
	driver, _ := setupRedis(t)
	ctx := context.Background()

	locked, err := driver.IsLocked(ctx, "lk4")
	require.NoError(t, err)
	assert.False(t, locked)

	_, _ = driver.AcquireLock(ctx, "lk4", time.Second)

	locked, err = driver.IsLocked(ctx, "lk4")
	require.NoError(t, err)
	assert.True(t, locked)
}

func TestRedisDriver_AcquireLock_ZeroTTL_UsesFallback(t *testing.T) {
	driver, mr := setupRedis(t)
	ok, err := driver.AcquireLock(context.Background(), "lk5", 0)
	require.NoError(t, err)
	require.True(t, ok)

	ttl := mr.TTL("lock:lk5")
	assert.True(t, ttl > 0)
}

// ---------------------------------------------------------------------------
// Session (end-to-end with Redis)
// ---------------------------------------------------------------------------

func TestRedisSession_CreateOrGet_EndToEnd(t *testing.T) {
	s, _, _ := setupRedisSession(t)
	ctx := context.Background()

	key := session.NewKey("test", "e2e", "1")
	first, err := s.CreateOrGet(ctx, key, session.SessionData{"user": "Emilia"})
	require.NoError(t, err)

	second, err := s.CreateOrGet(ctx, key, session.SessionData{"user": "bob"})
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "must return cached entry")
	assert.Equal(t, "Emilia", second.Get("user"))
}

func TestRedisSession_Force_EndToEnd(t *testing.T) {
	s, _, _ := setupRedisSession(t)
	ctx := context.Background()

	key := session.NewKey("test", "e2e", "2")
	original, err := s.CreateOrGet(ctx, key, session.SessionData{"user": "Emilia"})
	require.NoError(t, err)

	forced, err := s.Force(ctx, key, session.SessionData{"user": "carol"})
	require.NoError(t, err)
	assert.NotEqual(t, original.ID, forced.ID)
	assert.Equal(t, "carol", forced.Get("user"))
}

func TestRedisSession_TryLockWithLock_EndToEnd(t *testing.T) {
	s, _, _ := setupRedisSession(t)
	ctx := context.Background()
	key := session.NewKey("lock", "e2e", "1")

	// WithLock executes callback
	called := false
	ok, err := s.WithLock(ctx, key, func() error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, called)

	// lock auto-released — can acquire again
	ok2, err := s.TryLock(ctx, key)
	assert.NoError(t, err)
	assert.True(t, ok2)
}

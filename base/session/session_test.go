package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi/base/session"
)

// ---------------------------------------------------------------------------
// Fake in-memory Driver — no external dependencies
// ---------------------------------------------------------------------------

type fakeDriver struct {
	mu      sync.RWMutex
	entries map[string]*session.Entry
	locks   map[string]bool
	// hooks for error injection
	saveErr error
	getErr  error
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{
		entries: make(map[string]*session.Entry),
		locks:   make(map[string]bool),
	}
}

func (f *fakeDriver) Save(_ context.Context, key string, e *session.Entry) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[key] = e
	return nil
}

func (f *fakeDriver) Get(_ context.Context, key string) (*session.Entry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.entries[key], nil
}

func (f *fakeDriver) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, key)
	return nil
}

func (f *fakeDriver) ScanAll(_ context.Context, prefix string) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var keys []string
	for k := range f.entries {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (f *fakeDriver) CleanExpired(ctx context.Context, prefix string) error {
	keys, _ := f.ScanAll(ctx, prefix)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if e, ok := f.entries[k]; ok && e.IsExpired() {
			delete(f.entries, k)
		}
	}
	return nil
}

func (f *fakeDriver) AcquireLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locks[key] {
		return false, nil
	}
	f.locks[key] = true
	return true, nil
}

func (f *fakeDriver) ReleaseLock(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.locks, key)
	return nil
}

func (f *fakeDriver) IsLocked(_ context.Context, key string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.locks[key], nil
}

// lockKey returns the internal lock key that withLock uses, so tests can
// pre-lock a key to force the retry / context-cancellation path.
func lockKey(prefix, key string) string {
	return prefix + ":lock:" + key
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setup(t *testing.T) (*session.Session, *fakeDriver) {
	t.Helper()
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)

	d := newFakeDriver()
	cfg := &session.Config{Prefix: "test", TTL: 10 * time.Minute}
	s := session.New(d, cfg)
	return s, d
}

func data() session.SessionData { return session.SessionData{"user": "Emilia"} }

// ---------------------------------------------------------------------------
// Instance
// ---------------------------------------------------------------------------

func TestInstance_NilBeforeNew(t *testing.T) {
	session.ResetSingleton()
	defer session.ResetSingleton()
	assert.Nil(t, session.Instance())
}

func TestInstance_ReturnsSingletonAfterNew(t *testing.T) {
	s, _ := setup(t)
	assert.Equal(t, s, session.Instance())
}

func TestNew_SingletonIgnoresSubsequentCalls(t *testing.T) {
	s1, d1 := setup(t)
	d2 := newFakeDriver()
	s2 := session.New(d2, session.DefaultSessionConfig())
	assert.Equal(t, s1, s2, "second New call must return the same instance")
	_ = d1 // ensure first driver is the active one (compile check)
}

// ---------------------------------------------------------------------------
// CreateOrGet
// ---------------------------------------------------------------------------

func TestCreateOrGet_CreatesNewEntry(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "user", "1")

	entry, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.NotEmpty(t, entry.ID)
}

func TestCreateOrGet_ReturnsExistingNonExpired(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "user", "2")

	first, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)

	second, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "must return the same entry")
}

func TestCreateOrGet_ReplacesExpiredEntry(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "user", "3")

	// force an already-expired entry into the driver
	now := time.Now()
	expired := &session.Entry{
		ID:        "old-id",
		Data:      data(),
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(-time.Second),
	}
	d := newFakeDriver()
	d.entries[key] = expired

	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)
	s = session.New(d, &session.Config{Prefix: "test", TTL: 10 * time.Minute})

	fresh, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)
	assert.NotEqual(t, "old-id", fresh.ID)
}

func TestCreateOrGet_DriverGetError(t *testing.T) {
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)

	d := newFakeDriver()
	d.getErr = errors.New("redis down")
	s := session.New(d, &session.Config{Prefix: "test", TTL: time.Minute})

	_, err := s.CreateOrGet(context.Background(), "key", data())
	assert.Error(t, err)
}

func TestCreateOrGet_DriverSaveError(t *testing.T) {
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)

	d := newFakeDriver()
	d.saveErr = errors.New("disk full")
	s := session.New(d, &session.Config{Prefix: "test", TTL: time.Minute})

	_, err := s.CreateOrGet(context.Background(), "key", data())
	assert.Error(t, err)
}

func TestCreateOrGet_ContextCancelledWhileWaitingForLock(t *testing.T) {
	s, d := setup(t)
	key := session.NewKey("test", "user", "cancel")

	// hold the internal lock so the retry loop kicks in
	d.locks[lockKey("test", key)] = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.CreateOrGet(ctx, key, data())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCreateOrGet_ConcurrentSameKey(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "concurrent", "1")

	const n = 20
	ids := make(chan string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			e, err := s.CreateOrGet(context.Background(), key, data())
			if err == nil {
				ids <- e.ID
			}
		}()
	}
	wg.Wait()
	close(ids)

	var all []string
	for id := range ids {
		all = append(all, id)
	}
	require.Len(t, all, n)
	for _, id := range all {
		assert.Equal(t, all[0], id, "all goroutines must get the same entry")
	}
}

// ---------------------------------------------------------------------------
// Force
// ---------------------------------------------------------------------------

func TestForce_OverwritesExisting(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "user", "force1")

	original, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)

	forced, err := s.Force(context.Background(), key, session.SessionData{"user": "bob"})
	require.NoError(t, err)

	assert.NotEqual(t, original.ID, forced.ID)
	assert.Equal(t, "bob", forced.Get("user"))
}

func TestForce_SaveError(t *testing.T) {
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)

	d := newFakeDriver()
	d.saveErr = errors.New("disk full")
	s := session.New(d, &session.Config{Prefix: "test", TTL: time.Minute})

	_, err := s.Force(context.Background(), "key", data())
	assert.Error(t, err)
}

func TestForce_ContextCancelled(t *testing.T) {
	s, d := setup(t)
	key := session.NewKey("test", "user", "force-cancel")

	d.locks[lockKey("test", key)] = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Force(ctx, key, data())
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Get / Delete
// ---------------------------------------------------------------------------

func TestGet_ReturnsNilForMissingKey(t *testing.T) {
	s, _ := setup(t)
	entry, err := s.Get(context.Background(), "no-such-key")
	assert.NoError(t, err)
	assert.Nil(t, entry)
}

func TestGet_ReturnsStoredEntry(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "get", "1")

	created, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)

	got, err := s.Get(context.Background(), key)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

func TestDelete_RemovesEntry(t *testing.T) {
	s, _ := setup(t)
	key := session.NewKey("test", "del", "1")

	_, err := s.CreateOrGet(context.Background(), key, data())
	require.NoError(t, err)

	require.NoError(t, s.Delete(context.Background(), key))

	got, err := s.Get(context.Background(), key)
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDelete_NonExistentKeyIsNoOp(t *testing.T) {
	s, _ := setup(t)
	assert.NoError(t, s.Delete(context.Background(), "ghost"))
}

// ---------------------------------------------------------------------------
// CleanExpired
// ---------------------------------------------------------------------------

func TestCleanExpired_RemovesExpiredKeepsValid(t *testing.T) {
	s, d := setup(t)
	ctx := context.Background()

	now := time.Now()
	d.entries["test:valid"] = &session.Entry{ID: "v", ExpiresAt: now.Add(time.Hour)}
	d.entries["test:expired"] = &session.Entry{ID: "e", ExpiresAt: now.Add(-time.Second)}

	require.NoError(t, s.CleanExpired(ctx, "test:"))

	assert.NotNil(t, d.entries["test:valid"])
	assert.Nil(t, d.entries["test:expired"])
}

// ---------------------------------------------------------------------------
// TryLock / Unlock / IsLocked / WithLock
// ---------------------------------------------------------------------------

func TestTryLock_EmptyKey(t *testing.T) {
	s, _ := setup(t)
	ok, err := s.TryLock(context.Background(), "")
	assert.ErrorIs(t, err, session.ErrInvalidKey)
	assert.False(t, ok)
}

func TestUnlock_EmptyKey(t *testing.T) {
	s, _ := setup(t)
	assert.ErrorIs(t, s.Unlock(context.Background(), ""), session.ErrInvalidKey)
}

func TestIsLocked_EmptyKey(t *testing.T) {
	s, _ := setup(t)
	ok, err := s.IsLocked(context.Background(), "")
	assert.ErrorIs(t, err, session.ErrInvalidKey)
	assert.False(t, ok)
}

func TestTryLockUnlock_Cycle(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	key := session.NewKey("lock", "resource", "42")

	ok, err := s.TryLock(ctx, key)
	require.NoError(t, err)
	assert.True(t, ok)

	// already locked
	ok, err = s.TryLock(ctx, key)
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, s.Unlock(ctx, key))

	ok, err = s.TryLock(ctx, key)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsLocked_ReflectsState(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	key := session.NewKey("lock", "resource", "43")

	locked, err := s.IsLocked(ctx, key)
	require.NoError(t, err)
	assert.False(t, locked)

	_, _ = s.TryLock(ctx, key)

	locked, err = s.IsLocked(ctx, key)
	require.NoError(t, err)
	assert.True(t, locked)
}

func TestWithLock_ExecutesCallback(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	key := session.NewKey("lock", "withlock", "1")

	called := false
	ok, err := s.WithLock(ctx, key, func() error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, called)
}

func TestWithLock_SkipsCallbackIfAlreadyLocked(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	key := session.NewKey("lock", "withlock", "2")

	_, _ = s.TryLock(ctx, key)

	called := false
	ok, err := s.WithLock(ctx, key, func() error {
		called = true
		return nil
	})

	assert.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, called)
}

func TestWithLock_ReleasesLockAfterCallbackError(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	key := session.NewKey("lock", "withlock", "3")

	boom := errors.New("boom")
	ok, err := s.WithLock(ctx, key, func() error { return boom })
	assert.ErrorIs(t, err, boom)
	assert.True(t, ok)

	// lock must have been released
	locked, _ := s.IsLocked(ctx, key)
	assert.False(t, locked)
}

func TestSession_ImplementsDistributedLocker(t *testing.T) {
	s, _ := setup(t)
	var _ session.DistributedLocker = s // compile-time assertion
}

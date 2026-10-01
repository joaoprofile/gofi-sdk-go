package cache

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("cache-test")
	os.Exit(m.Run())
}

// Helpers

// withRedis starts a miniredis server, makes it the shared client, and
// restores the previous one when the test finishes.
func withRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	prev := current.Load()
	current.Store(&shared{client: redis.NewClient(&redis.Options{Addr: mr.Addr()})})
	t.Cleanup(func() { current.Store(prev) })
	return mr
}

// withNoRedis leaves the shared client unset for the duration of the test.
func withNoRedis(t *testing.T) {
	t.Helper()
	prev := current.Load()
	current.Store(nil)
	t.Cleanup(func() { current.Store(prev) })
}

// withConfig points lazy creation at mr with no shared client yet.
func withConfig(t *testing.T, mr *miniredis.Miniredis) {
	t.Helper()
	prevCfg := cfg
	Configure(Config{URI: mr.Addr()})
	withNoRedis(t)
	t.Cleanup(func() { cfg = prevCfg })
}

// NewCache
func TestNewCache_ReturnsNonNil(t *testing.T) {
	c := NewCache[string]("mykey", time.Minute)
	assert.NotNil(t, c)
}

func TestNewCache_StoresNameAndTTL(t *testing.T) {
	c := NewCache[int]("counter", 5*time.Second)
	assert.Equal(t, "counter", c.name)
	assert.Equal(t, 5*time.Second, c.ttl)
}

// validate
func TestValidate_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)
	err := c.validate()
	assert.EqualError(t, err, "Cache not initialized")
}

func TestValidate_EmptyName_ReturnsError(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("", time.Minute)
	err := c.validate()
	assert.EqualError(t, err, "Cache without name")
}

func TestValidate_Valid_NoError(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("k", time.Minute)
	err := c.validate()
	assert.NoError(t, err)
}

// getNamePrefixed
func TestGetNamePrefixed_ContainsNameAndSeparator(t *testing.T) {
	c := NewCache[string]("my-cache-key", time.Minute)
	prefixed := c.getNamePrefixed()
	assert.Contains(t, prefixed, "my-cache-key")
	assert.Contains(t, prefixed, "::")
}

// List — validate fails → returns error
func TestList_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)
	result, err := c.List(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// UniqueResult — validate fails → returns error
func TestUniqueResult_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)
	result, err := c.UniqueResult(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// Set — validate fails → returns error
func TestSet_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)
	err := c.Set(context.Background(), "value")
	assert.Error(t, err)
}

// Del — validate fails → returns error
func TestDel_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)
	err := c.Del(context.Background())
	assert.Error(t, err)
}

// List — cache miss (key absent) → nil, nil
func TestList_CacheMiss_ReturnsNilNoError(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("missing-key", time.Minute)
	result, err := c.List(context.Background())
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// UniqueResult — cache miss → nil, nil
func TestUniqueResult_CacheMiss_ReturnsNilNoError(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("missing-key", time.Minute)
	result, err := c.UniqueResult(context.Background())
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// Set then List — round-trip
func TestSetThenList_RoundTrip(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("items", time.Minute)
	data := []string{"apple", "banana"}

	require.NoError(t, c.Set(context.Background(), data))

	result, err := c.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

// Set then UniqueResult — round-trip
func TestSetThenUniqueResult_RoundTrip(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("item", time.Minute)

	require.NoError(t, c.Set(context.Background(), "hello"))

	result, err := c.UniqueResult(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "hello", *result)
}

// Set then Get into arbitrary dest — round-trip
func TestSetThenGet_RoundTrip(t *testing.T) {
	withRedis(t)
	type page struct {
		Total   int      `json:"total"`
		Content []string `json:"content"`
	}
	c := NewCache[string]("paged", time.Minute)
	want := page{Total: 2, Content: []string{"a", "b"}}

	require.NoError(t, c.Set(context.Background(), &want))

	var got page
	hit, err := c.Get(context.Background(), &got)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, want, got)
}

func TestGet_CacheMiss_ReturnsFalseNoError(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("absent", time.Minute)

	var got string
	hit, err := c.Get(context.Background(), &got)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestGet_NilRedis_ReturnsError(t *testing.T) {
	withNoRedis(t)
	c := NewCache[string]("k", time.Minute)

	var got string
	hit, err := c.Get(context.Background(), &got)
	assert.False(t, hit)
	assert.Error(t, err)
}

// Set then Del — key removed
func TestSetThenDel_KeyRemoved(t *testing.T) {
	withRedis(t)
	c := NewCache[string]("to-delete", time.Minute)

	require.NoError(t, c.Set(context.Background(), "value"))
	require.NoError(t, c.Del(context.Background()))

	result, err := c.List(context.Background())
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// Set — json marshal error (channels cannot be marshalled)
func TestSet_MarshalError_ReturnsError(t *testing.T) {
	withRedis(t)
	c := NewCache[any]("k", time.Minute)
	err := c.Set(context.Background(), make(chan int))
	assert.Error(t, err)
}

// get — non-errRedisNil error (WRONGTYPE when key holds a hash, not a string)

func TestGet_NonRedisNilError_ReturnsError(t *testing.T) {
	mr := withRedis(t)
	c := NewCache[string]("wrongtype-key", time.Minute)
	mr.HSet(c.getNamePrefixed(), "field", "value") // creates a hash — GET returns WRONGTYPE
	result, err := c.get(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// List — error propagated from get (WRONGTYPE)

func TestList_GetError_ReturnsError(t *testing.T) {
	mr := withRedis(t)
	c := NewCache[string]("list-wrongtype", time.Minute)
	mr.HSet(c.getNamePrefixed(), "field", "value")
	result, err := c.List(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// List — JSON unmarshal error (key exists but holds invalid JSON)

func TestList_UnmarshalError_ReturnsError(t *testing.T) {
	mr := withRedis(t)
	c := NewCache[string]("list-badjson", time.Minute)
	mr.Set(c.getNamePrefixed(), `{not valid json at all}`)
	result, err := c.List(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// UniqueResult — error propagated from get (WRONGTYPE)

func TestUniqueResult_GetError_ReturnsError(t *testing.T) {
	mr := withRedis(t)
	c := NewCache[string]("unique-wrongtype", time.Minute)
	mr.HSet(c.getNamePrefixed(), "field", "value")
	result, err := c.UniqueResult(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// UniqueResult — JSON unmarshal error

func TestUniqueResult_UnmarshalError_ReturnsError(t *testing.T) {
	mr := withRedis(t)
	c := NewCache[string]("unique-badjson", time.Minute)
	mr.Set(c.getNamePrefixed(), `{not valid json at all}`)
	result, err := c.UniqueResult(context.Background())
	assert.Nil(t, result)
	assert.Error(t, err)
}

// Shared client lifecycle

func TestInstanceRedis_CreatesOnceFromConfig(t *testing.T) {
	withConfig(t, miniredis.RunT(t))
	first := InstanceRedis()
	require.NotNil(t, first)
	assert.Same(t, first, InstanceRedis())
	assert.NoError(t, Ping(context.Background()))
}

func TestInstanceRedis_ConcurrentFirstUseCreatesOneClient(t *testing.T) {
	withConfig(t, miniredis.RunT(t))
	clients := make(chan redis.UniversalClient, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { clients <- InstanceRedis() })
	}
	wg.Wait()
	close(clients)
	first := <-clients
	for c := range clients {
		assert.Same(t, first, c)
	}
}

// After Close the shared client stays closed instead of dialing again.
func TestClose_DoesNotReconnect(t *testing.T) {
	withConfig(t, miniredis.RunT(t))
	client := InstanceRedis()
	require.NoError(t, Close())
	require.NoError(t, Close(), "Close is idempotent")
	assert.Same(t, client, InstanceRedis())
	assert.ErrorIs(t, Ping(context.Background()), redis.ErrClosed)
}

// Injected clients belong to the caller.
func TestClose_LeavesInjectedClientOpen(t *testing.T) {
	withNoRedis(t)
	mr := miniredis.RunT(t)
	injected := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { injected.Close() })
	UseClient(injected)
	require.NoError(t, Close())
	assert.NoError(t, injected.Ping(context.Background()).Err())
}

func TestNewCacheRedis_UnreachableOnlyLogs(t *testing.T) {
	prevCfg := cfg
	Configure(Config{URI: "127.0.0.1:1"})
	withNoRedis(t)
	t.Cleanup(func() { cfg = prevCfg })
	assert.NotPanics(t, NewCacheRedis)
	assert.Error(t, Ping(context.Background()))
}

func TestCache_WithClientUsesItsOwnRedis(t *testing.T) {
	withRedis(t) // shared
	own := miniredis.RunT(t)
	c := NewCache[string]("own", time.Minute).WithClient(redis.NewClient(&redis.Options{Addr: own.Addr()}))
	require.NoError(t, c.SetKeyed(context.Background(), "k", "v"))
	assert.NotEmpty(t, own.Keys(), "written to the cache's own Redis")
}

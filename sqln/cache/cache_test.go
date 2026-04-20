package cache

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("cache-test")
	os.Exit(m.Run())
}

// Helpers

// withRedis starts a miniredis server, wires it into redisInstance, and cleans
// up when the test finishes.
func withRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	prev := redisInstance
	redisInstance = &singletonRedis{
		client: redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		once:   sync.Once{},
	}
	t.Cleanup(func() { redisInstance = prev })
	return mr
}

// withNoRedis ensures redisInstance is nil for the duration of the test.
func withNoRedis(t *testing.T) {
	t.Helper()
	prev := redisInstance
	redisInstance = nil
	t.Cleanup(func() { redisInstance = prev })
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

// NewCacheRedis — initializes the singleton client using environment CacheURI

func TestNewCacheRedis_InitializesClient(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("CACHE_URI", mr.Addr())
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	prev := redisInstance
	redisInstance = nil
	t.Cleanup(func() { redisInstance = prev })

	NewCacheRedis()

	require.NotNil(t, redisInstance)
	assert.NotNil(t, redisInstance.client)
}

// NewCacheRedis — idempotent: second call is a no-op (once.Do)

func TestNewCacheRedis_Idempotent_SecondCallIsNoOp(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("CACHE_URI", mr.Addr())
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	prev := redisInstance
	redisInstance = nil
	t.Cleanup(func() { redisInstance = prev })

	NewCacheRedis()
	first := redisInstance.client

	NewCacheRedis()
	// once.Do ensures the second call does not replace the client.
	assert.Equal(t, first, redisInstance.client)
}

// InstanceRedis — client==nil branch triggers NewCacheRedis

func TestInstanceRedis_ClientNil_TriggersNewCacheRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("CACHE_URI", mr.Addr())
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	prev := redisInstance
	redisInstance = &singletonRedis{client: nil, once: sync.Once{}}
	t.Cleanup(func() { redisInstance = prev })

	client := InstanceRedis()
	assert.NotNil(t, client)
}

// cacheDBObserver.Close — closes the client and nils redisInstance

func TestCacheDBObserver_Close_ClosesConnectionAndNilsInstance(t *testing.T) {
	withRedis(t)
	// withRedis cleanup restores the previous redisInstance after the test.

	obs := &cacheDBObserver{}
	obs.Close()

	assert.Nil(t, redisInstance)
}

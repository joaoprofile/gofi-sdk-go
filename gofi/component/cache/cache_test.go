package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	sqlncache "github.com/joaoprofile/gofi-sdk-go/sqln/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetSharedRedis isolates the process-wide cache client between tests.
func resetSharedRedis(t *testing.T) {
	t.Helper()
	logging.NewLogger("test")
	sqlncache.UseClient(nil)
	t.Cleanup(func() { sqlncache.UseClient(nil) })
}

func TestNewOpensOnStartAndClosesWithRuntime(t *testing.T) {
	resetSharedRedis(t)
	mr := miniredis.RunT(t)
	rt := gofi.NewRuntime(&environment.Environment{CacheURI: mr.Addr()})
	c := New()
	assert.Nil(t, c.Client(), "New only declares")

	require.NoError(t, c.Start(context.Background(), rt))
	require.NotNil(t, c.Client())
	require.NoError(t, c.Client().Ping(context.Background()).Err())
	require.Len(t, rt.HealthChecks(), 1)
	assert.Equal(t, "cache", rt.HealthChecks()[0].Name)

	require.NoError(t, rt.Close(context.Background()))
	assert.ErrorIs(t, c.Client().Ping(context.Background()).Err(), redis.ErrClosed)
}

// An unreachable Redis fails Start instead of starting without cache.
func TestNewUnreachableFailsStart(t *testing.T) {
	resetSharedRedis(t)
	rt := gofi.NewRuntime(&environment.Environment{CacheURI: "127.0.0.1:1"})
	assert.ErrorContains(t, New().Start(context.Background(), rt), "redis 127.0.0.1:1")
}

func TestFromClientIsSharedAndNotClosed(t *testing.T) {
	resetSharedRedis(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	rt := gofi.NewRuntime(&environment.Environment{})

	c := FromClient(client)
	require.NoError(t, c.Start(context.Background(), rt))
	shared, err := Shared(rt)
	require.NoError(t, err)
	assert.Same(t, client, shared, "session reuses the injected client")
	assert.Len(t, rt.HealthChecks(), 1)

	require.NoError(t, rt.Close(context.Background()))
	assert.NoError(t, client.Ping(context.Background()).Err(), "the caller owns FromClient clients")
}

func TestSharedOpensOnce(t *testing.T) {
	resetSharedRedis(t)
	mr := miniredis.RunT(t)
	rt := gofi.NewRuntime(&environment.Environment{CacheURI: mr.Addr()})

	first, err := Shared(rt)
	require.NoError(t, err)
	second, err := Shared(rt)
	require.NoError(t, err)
	assert.Same(t, first, second)
	assert.Len(t, rt.HealthChecks(), 1)
}

func TestFromClientNilIsAccepted(t *testing.T) {
	resetSharedRedis(t)
	rt := gofi.NewRuntime(&environment.Environment{})
	c := FromClient(nil)
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Nil(t, c.Client())
	assert.Empty(t, rt.HealthChecks())
}

func TestInsecureTransports(t *testing.T) {
	logging.NewLogger("test")
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "prod", CacheURI: "redis:6379"}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, "cache", found[0].Resource)
	assert.ErrorIs(t, core.CheckTransport(env, found), core.ErrInsecureTransport, "refused in prod")
	env.AllowInsecureTransport = "cache"
	assert.NoError(t, core.CheckTransport(env, found), "allowed by the hatch")

	assert.Empty(t, FromClient(nil).InsecureTransports(env), "injected clients are the caller's")
	env.CacheUseTLS = true
	assert.Empty(t, New().InsecureTransports(env))
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "cache", New().Name())
	assert.Equal(t, gofi.StageCache, New().Stage())
}

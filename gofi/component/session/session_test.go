package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	basesession "github.com/joaoprofile/gofi-sdk-go/base/session"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	sqlncache "github.com/joaoprofile/gofi-sdk-go/sqln/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUsesDefaultConfig(t *testing.T) {
	assert.Equal(t, basesession.DefaultSessionConfig(), New().Config())
	var nilCfg *basesession.Config
	assert.Equal(t, basesession.DefaultSessionConfig(), New(nilCfg).Config())

	custom := &basesession.Config{TTL: time.Minute}
	assert.Same(t, custom, New(custom).Config())
}

func TestStartRejectsUnsupportedCacheTypes(t *testing.T) {
	oci := gofi.NewRuntime(&environment.Environment{CacheType: string(environment.OCI_CACHE)})
	assert.ErrorContains(t, New().Start(context.Background(), oci), "OCI session driver is not implemented")

	unknown := gofi.NewRuntime(&environment.Environment{CacheType: "unknown"})
	assert.ErrorContains(t, New().Start(context.Background(), unknown), `unsupported CACHE_TYPE "unknown"`)
}

// Without a cache component, the session opens the shared Redis client itself.
func TestStartWithRedisOpensSharedClient(t *testing.T) {
	logging.NewLogger("test")
	sqlncache.UseClient(nil)
	t.Cleanup(func() { sqlncache.UseClient(nil) })
	mr := miniredis.RunT(t)
	rt := gofi.NewRuntime(&environment.Environment{CacheType: string(environment.REDIS_CACHE), CacheURI: mr.Addr()})

	require.NoError(t, New().Start(context.Background(), rt))
	assert.NotNil(t, basesession.Instance())
	assert.Len(t, rt.HealthChecks(), 1)
	require.NoError(t, rt.Close(context.Background()))
}

func TestStartWithUnreachableRedisFails(t *testing.T) {
	logging.NewLogger("test")
	sqlncache.UseClient(nil)
	t.Cleanup(func() { sqlncache.UseClient(nil) })
	rt := gofi.NewRuntime(&environment.Environment{CacheType: string(environment.REDIS_CACHE), CacheURI: "127.0.0.1:1"})
	assert.ErrorContains(t, New().Start(context.Background(), rt), "redis 127.0.0.1:1")
}

func TestInsecureTransports(t *testing.T) {
	logging.NewLogger("test")
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "stage", CacheType: "redis", CacheURI: "redis:6379"}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, "cache", found[0].Resource)
	assert.ErrorIs(t, core.CheckTransport(env, found), core.ErrInsecureTransport, "refused in stage")
	env.AllowInsecureTransport = "all"
	assert.NoError(t, core.CheckTransport(env, found), "allowed by the hatch")

	env.CacheType = "oci"
	assert.Empty(t, New().InsecureTransports(env), "no Redis without CACHE_TYPE=redis")
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "session", New().Name())
	assert.Equal(t, gofi.StageSession, New().Stage())
}

package gofi_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/cache"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/database"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/httpserver"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/session"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	sqlncache "github.com/joaoprofile/gofi-sdk-go/sqln/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

type recordingServer struct{ health []string }

func (s *recordingServer) ListenAndServe() error          { return nil }
func (s *recordingServer) Shutdown(context.Context) error { return nil }
func (s *recordingServer) AddHealthCheck(name string, _ func(context.Context) error) {
	s.health = append(s.health, name)
}
func (s *recordingServer) AddHandlers(...netx.RouterHandler) {}
func (s *recordingServer) Use(...netx.Middleware)            {}
func (s *recordingServer) UseAuth(netx.Middleware)           {}

// The components are declared out of order; Build starts them by stage, cache
// and session share one Redis client, and the HTTP server gets every check.
func TestBuildWithComponents(t *testing.T) {
	mr := miniredis.RunT(t)
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)
	sqlncache.UseClient(nil)
	t.Cleanup(func() { sqlncache.UseClient(nil) })
	t.Setenv("APP_ENVIRONMENT", "dev")
	t.Setenv("CACHE_TYPE", "redis")
	t.Setenv("CACHE_URI", mr.Addr())

	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	srv := &recordingServer{}
	c := cache.New()
	svc, err := gofi.New("integration").With(
		httpserver.FromServer(srv),
		session.New(),
		c,
		database.FromDB(db),
	).Build()
	require.NoError(t, err)

	assert.Equal(t, []string{"database", "cache"}, srv.health)
	require.NoError(t, c.Client().Ping(context.Background()).Err())

	require.NoError(t, svc.Shutdown(context.Background()))
	assert.ErrorIs(t, c.Client().Ping(context.Background()).Err(), redis.ErrClosed)
}

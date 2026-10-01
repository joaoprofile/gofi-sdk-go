package netx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func probe(h http.Handler, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestHealth_Endpoints(t *testing.T) {
	ws := &httpServer{config: &WSConfig{Health: &HealthConfig{}}, router: chi.NewMux()}
	ws.router.Get("/api", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := ws.handler()

	assert.Equal(t, http.StatusOK, probe(h, "/livez"))
	assert.Equal(t, http.StatusServiceUnavailable, probe(h, "/readyz"), "not ready before Run")
	assert.Equal(t, http.StatusTeapot, probe(h, "/api"), "other routes reach the router")

	ws.ready.Store(true)
	assert.Equal(t, http.StatusOK, probe(h, "/readyz"))

	ws.AddHealthCheck("db", func(context.Context) error { return errors.New("down") })
	assert.Equal(t, http.StatusServiceUnavailable, probe(h, "/readyz"), "failing check fails readiness")
}

// Regression: /readyz returned err.Error() of each check to the public.
func TestHealth_ReadinessHidesErrors(t *testing.T) {
	ws := &httpServer{config: &WSConfig{Health: &HealthConfig{}}, router: chi.NewMux()}
	ws.ready.Store(true)
	ws.AddHealthCheck("db", func(context.Context) error {
		return errors.New("dial tcp 10.0.0.7:5432: password authentication failed")
	})
	ws.AddHealthCheck("cache", func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	ws.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"unavailable","checks":{"db":"fail"}}`, rec.Body.String())
}

// Regression: every probe ran every check, unbounded.
func TestHealth_ReadinessCachedAndShared(t *testing.T) {
	ws := &httpServer{config: &WSConfig{Health: &HealthConfig{CacheTTL: time.Hour}}, router: chi.NewMux()}
	ws.ready.Store(true)
	var calls atomic.Int32
	release := make(chan struct{})
	ws.AddHealthCheck("db", func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	})
	h := ws.handler()

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { assert.Equal(t, http.StatusOK, probe(h, "/readyz")) })
	}
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the other probes queue on the running round
	close(release)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load(), "concurrent probes share one round")

	assert.Equal(t, http.StatusOK, probe(h, "/readyz"))
	assert.Equal(t, int32(1), calls.Load(), "fresh result is cached")
}

func TestHealth_ReadinessTimeoutAndPanic(t *testing.T) {
	ws := &httpServer{config: &WSConfig{Health: &HealthConfig{CheckTimeout: 20 * time.Millisecond}}, router: chi.NewMux()}
	ws.ready.Store(true)
	block := make(chan struct{})
	defer close(block)
	ws.AddHealthCheck("stuck", func(context.Context) error { <-block; return nil }) // ignores ctx
	ws.AddHealthCheck("panics", func(context.Context) error { panic("boom") })
	ws.AddHealthCheck("ok", func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	start := time.Now()
	ws.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Less(t, time.Since(start), time.Second, "a stuck check is bounded by CheckTimeout")
	assert.JSONEq(t, `{"status":"unavailable","checks":{"stuck":"fail","panics":"fail"}}`, rec.Body.String())
}

func TestHealth_DisabledByDefault(t *testing.T) {
	ws := &httpServer{config: &WSConfig{}, router: chi.NewMux()}
	assert.Equal(t, http.StatusNotFound, probe(ws.handler(), "/livez"))
}

func TestHealth_CustomPathsAndDrainDefault(t *testing.T) {
	ws := &httpServer{config: &WSConfig{Health: &HealthConfig{LivenessPath: "/healthz"}}, router: chi.NewMux()}
	assert.Equal(t, http.StatusOK, probe(ws.handler(), "/healthz"))
	assert.Equal(t, DefaultDrainDelay, ws.drainDelay())
	assert.Zero(t, (&httpServer{config: &WSConfig{}}).drainDelay())
}

package netx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── stub backend ─────────────────────────────────────────────────────────────

type stubBackend struct {
	fn func(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error)
}

func (s *stubBackend) Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error) {
	return s.fn(ctx, key, limit)
}

func allowedResult(remaining int) *redis_rate.Result {
	return &redis_rate.Result{
		Allowed:    1,
		Remaining:  remaining,
		ResetAfter: time.Second,
		RetryAfter: -1,
	}
}

func blockedResult() *redis_rate.Result {
	return &redis_rate.Result{
		Allowed:    0,
		Remaining:  0,
		ResetAfter: time.Second,
		RetryAfter: 500 * time.Millisecond,
	}
}

func defaultConfig(backend RateLimiterBackend) RedisRateLimiterConfig {
	return RedisRateLimiterConfig{
		Backend:   backend,
		KeyPrefix: "rl",
		Default:   RateLimitPlan{Rate: 10, Burst: 10},
		Plans: map[string]RateLimitPlan{
			"premium": {Rate: 100, Burst: 200},
		},
	}
}

func newMiniredisBackend(t *testing.T) (RateLimiterBackend, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return NewRedisBackend(client), mr
}

// ── NewRedisRateLimiter ───────────────────────────────────────────────────────

func TestRateLimiter_AllowedRequest_CallsNextAndSetsHeaders(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return allowedResult(9), nil
	}}

	nextCalled := false
	mw := NewRedisRateLimiter(defaultConfig(backend))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "10", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "9", rec.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
}

func TestRateLimiter_BlockedRequest_Returns429(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return blockedResult(), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestRateLimiter_BlockedRequest_DoesNotCallNext(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return blockedResult(), nil
	}}

	nextCalled := false
	mw := NewRedisRateLimiter(defaultConfig(backend))
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assert.False(t, nextCalled)
}

func TestRateLimiter_BackendError_FailOpen(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return nil, errors.New("redis: connection refused")
	}}

	nextCalled := false
	mw := NewRedisRateLimiter(defaultConfig(backend))
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, nextCalled, "fail-open: next must be called on backend error")
}

func TestRateLimiter_APIKey_UsedAsClientID(t *testing.T) {
	var capturedKey string
	backend := &stubBackend{fn: func(_ context.Context, key string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		capturedKey = key
		return allowedResult(5), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "rl:my-api-key", capturedKey)
}

func TestRateLimiter_NoAPIKey_IPUsedAsClientID(t *testing.T) {
	var capturedKey string
	backend := &stubBackend{fn: func(_ context.Context, key string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		capturedKey = key
		return allowedResult(5), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "rl:192.168.1.1", capturedKey)
}

func TestRateLimiter_NamedPlan_AppliedWhenAPIKeyMatches(t *testing.T) {
	var capturedLimit redis_rate.Limit
	backend := &stubBackend{fn: func(_ context.Context, _ string, limit redis_rate.Limit) (*redis_rate.Result, error) {
		capturedLimit = limit
		return allowedResult(199), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "premium")
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 100, capturedLimit.Rate)
	assert.Equal(t, 200, capturedLimit.Burst)
}

func TestRateLimiter_DefaultPlan_WhenNoMatchingAPIKey(t *testing.T) {
	var capturedLimit redis_rate.Limit
	backend := &stubBackend{fn: func(_ context.Context, _ string, limit redis_rate.Limit) (*redis_rate.Result, error) {
		capturedLimit = limit
		return allowedResult(9), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "unknown")
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 10, capturedLimit.Rate)
	assert.Equal(t, 10, capturedLimit.Burst)
}

func TestRateLimiter_EmptyKeyPrefix_DefaultsToRL(t *testing.T) {
	var capturedKey string
	backend := &stubBackend{fn: func(_ context.Context, key string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		capturedKey = key
		return allowedResult(5), nil
	}}

	cfg := RedisRateLimiterConfig{Backend: backend, KeyPrefix: "", Default: RateLimitPlan{Rate: 5, Burst: 5}}
	mw := NewRedisRateLimiter(cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:9000"
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "rl:10.0.0.1", capturedKey)
}

func TestRateLimiter_RetryAfterAtLeastOneSecond(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return &redis_rate.Result{Allowed: 0, RetryAfter: 100 * time.Millisecond}, nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, retryAfter, 1)
}

func TestRateLimiter_ContextPropagatedToBackend(t *testing.T) {
	type ctxKey struct{}
	var capturedCtx context.Context

	backend := &stubBackend{fn: func(ctx context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		capturedCtx = ctx
		return allowedResult(5), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "sentinel"))
	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "sentinel", capturedCtx.Value(ctxKey{}))
}

// ── setRateLimitHeaders ───────────────────────────────────────────────────────

func TestSetRateLimitHeaders_WritesExpectedHeaders(t *testing.T) {
	plan := RateLimitPlan{Rate: 50, Burst: 100}
	res := &redis_rate.Result{Remaining: 42, ResetAfter: 5 * time.Second}

	rec := httptest.NewRecorder()
	setRateLimitHeaders(rec, plan, res)

	assert.Equal(t, "50", rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "42", rec.Header().Get("X-RateLimit-Remaining"))

	resetVal, err := strconv.ParseInt(rec.Header().Get("X-RateLimit-Reset"), 10, 64)
	require.NoError(t, err)
	expected := time.Now().Add(5 * time.Second).Unix()
	assert.InDelta(t, expected, resetVal, 2)
}

// ── extractAPIKey ─────────────────────────────────────────────────────────────

func TestExtractAPIKey_ReturnsHeaderValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "test-key-123")
	assert.Equal(t, "test-key-123", extractAPIKey(req))
}

func TestExtractAPIKey_ReturnsEmptyWhenAbsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, extractAPIKey(req))
}

// ── extractIP ─────────────────────────────────────────────────────────────────

func TestExtractIP_UsesXForwardedForFirstEntry(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	assert.Equal(t, "1.2.3.4", extractIP(req))
}

func TestExtractIP_TrimsSpacesInXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "  203.0.113.5  , 10.0.0.1")
	assert.Equal(t, "203.0.113.5", extractIP(req))
}

func TestExtractIP_FallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.0.1:5000"
	assert.Equal(t, "192.168.0.1", extractIP(req))
}

func TestExtractIP_RemoteAddrWithoutPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.0.1"
	assert.Equal(t, "192.168.0.1", extractIP(req))
}

// ── NewRedisBackend ───────────────────────────────────────────────────────────

func TestNewRedisBackend_ReturnsFunctionalBackend(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	backend := NewRedisBackend(client)
	require.NotNil(t, backend)

	res, err := backend.Allow(context.Background(), "test:key", redis_rate.Limit{Rate: 10, Burst: 10, Period: time.Second})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Allowed)
}

// ── miniredis integration ─────────────────────────────────────────────────────

func TestRateLimiter_Miniredis_AllowsUpToBurst(t *testing.T) {
	backend, _ := newMiniredisBackend(t)
	cfg := RedisRateLimiterConfig{Backend: backend, KeyPrefix: "rl", Default: RateLimitPlan{Rate: 3, Burst: 3}}
	mw := NewRedisRateLimiter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:0"

	for i := range 3 {
		rec := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should be allowed", i+1)
	}
}

func TestRateLimiter_Miniredis_BlocksAfterBurstExhausted(t *testing.T) {
	backend, _ := newMiniredisBackend(t)
	cfg := RedisRateLimiterConfig{Backend: backend, KeyPrefix: "rl", Default: RateLimitPlan{Rate: 2, Burst: 2}}
	mw := NewRedisRateLimiter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:0"

	for range 2 {
		mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)
	}

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestRateLimiter_Miniredis_DifferentClientsAreIsolated(t *testing.T) {
	backend, _ := newMiniredisBackend(t)
	cfg := RedisRateLimiterConfig{Backend: backend, KeyPrefix: "rl", Default: RateLimitPlan{Rate: 1, Burst: 1}}
	mw := NewRedisRateLimiter(cfg)

	reqA := httptest.NewRequest(http.MethodGet, "/", nil)
	reqA.RemoteAddr = "10.0.0.10:0"
	reqB := httptest.NewRequest(http.MethodGet, "/", nil)
	reqB.RemoteAddr = "10.0.0.11:0"

	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), reqA)

	recA := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(recA, reqA)
	assert.Equal(t, http.StatusTooManyRequests, recA.Code)

	recB := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(recB, reqB)
	assert.Equal(t, http.StatusOK, recB.Code)
}

func TestRateLimiter_Miniredis_WindowResetsAfterPeriod(t *testing.T) {
	backend, mr := newMiniredisBackend(t)
	cfg := RedisRateLimiterConfig{Backend: backend, KeyPrefix: "rl", Default: RateLimitPlan{Rate: 1, Burst: 1}}
	mw := NewRedisRateLimiter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.20:0"

	mw(okHandler()).ServeHTTP(httptest.NewRecorder(), req)
	rec1 := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec1, req)
	assert.Equal(t, http.StatusTooManyRequests, rec1.Code)

	mr.FastForward(2 * time.Second)

	rec2 := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec2, req)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// ── all standard headers present ─────────────────────────────────────────────

func assertRateLimitHeadersPresent(t *testing.T, h http.Header) {
	t.Helper()
	for _, name := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"} {
		assert.NotEmpty(t, h.Get(name), fmt.Sprintf("header %s must be present", name))
	}
}

func TestRateLimiter_AllStandardHeadersPresentOnAllowed(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return allowedResult(7), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assertRateLimitHeadersPresent(t, rec.Header())
}

func TestRateLimiter_AllStandardHeadersPresentOnBlocked(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return blockedResult(), nil
	}}

	mw := NewRedisRateLimiter(defaultConfig(backend))
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assertRateLimitHeadersPresent(t, rec.Header())
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

package netx

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── SecurityHeaders ───────────────────────────────────────────────────────────

func TestSecurityHeaders_SetsAllRequiredHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	SecurityHeaders(okHandler()).ServeHTTP(rec, req)

	h := rec.Header()
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.Equal(t, "geolocation=(), microphone=(), camera=()", h.Get("Permissions-Policy"))
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "max-age=63072000; includeSubDomains; preload", h.Get("Strict-Transport-Security"))
	assert.Contains(t, h.Get("Content-Security-Policy"), "default-src 'self'")
	assert.Contains(t, h.Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

func TestSecurityHeaders_ClearsServerFingerprinting(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	SecurityHeaders(okHandler()).ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Server"))
	assert.Empty(t, rec.Header().Get("X-Powered-By"))
}

func TestSecurityHeaders_CallsNextHandler(t *testing.T) {
	called := false
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ── BlockUnsafeMethods ────────────────────────────────────────────────────────

func TestBlockUnsafeMethods_RejectsTRACE(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodTrace, "/", nil)
	BlockUnsafeMethods(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestBlockUnsafeMethods_RejectsCONNECT(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodConnect, "/", nil)
	BlockUnsafeMethods(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestBlockUnsafeMethods_DoesNotCallNextOnRejected(t *testing.T) {
	called := false
	handler := BlockUnsafeMethods(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodTrace, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.False(t, called)
}

func TestBlockUnsafeMethods_AllowsSafeMethods(t *testing.T) {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead,
	} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/", nil)
			BlockUnsafeMethods(okHandler()).ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// ── LimitBody ─────────────────────────────────────────────────────────────────

func TestLimitBody_CallsNextForSmallBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()

	called := false
	LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLimitBody_WrapsBodyWithMaxBytesReader(t *testing.T) {
	const overLimit = (10 << 20) + 1

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, overLimit)))
	rec := httptest.NewRecorder()

	var readErr error
	LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})).ServeHTTP(rec, req)

	require.Error(t, readErr, "reading past the limit must produce an error")
}

// ── ValidateRequest ───────────────────────────────────────────────────────────

func TestValidateRequest_RejectsSmugglingVector(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Length", "100")
	req.TransferEncoding = []string{"chunked"}

	rec := httptest.NewRecorder()
	ValidateRequest(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestValidateRequest_AllowsContentLengthOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Length", "10")

	rec := httptest.NewRecorder()
	ValidateRequest(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestValidateRequest_AllowsNeitherHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	rec := httptest.NewRecorder()
	ValidateRequest(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── newSemaphoreLimiter ───────────────────────────────────────────────────────

func TestSemaphoreLimiter_AllowsRequestsWithinLimit(t *testing.T) {
	mw := newSemaphoreLimiter(5, 100*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	mw(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSemaphoreLimiter_Returns503WhenFull(t *testing.T) {
	const max = 2
	mw := newSemaphoreLimiter(max, 20*time.Millisecond)

	started := make(chan struct{})
	unblock := make(chan struct{})

	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-unblock
		w.WriteHeader(http.StatusOK)
	})

	var wg sync.WaitGroup
	for range max {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
		<-started
	}

	rec := httptest.NewRecorder()
	mw(slowHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	close(unblock)
	wg.Wait()
}

func TestSemaphoreLimiter_DefaultsAppliedForZeroConfig(t *testing.T) {
	mw := newSemaphoreLimiter(0, 0)
	require.NotNil(t, mw)

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSemaphoreLimiter_ReleasesSlotAfterRequest(t *testing.T) {
	mw := newSemaphoreLimiter(1, 100*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rec1 := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec1, req)
	assert.Equal(t, http.StatusOK, rec1.Code)

	rec2 := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec2, req)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// ── NewStressControlMiddleware ────────────────────────────────────────────────

func TestStressControl_DefaultLimiterUsedWhenNoRouteMatch(t *testing.T) {
	cfg := StressControlConfig{DefaultMaxConcurrent: 10, DefaultTimeout: 100 * time.Millisecond}
	mw := NewStressControlMiddleware(cfg)

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStressControl_RouteSpecificLimiterUsedOnPrefixMatch(t *testing.T) {
	cfg := StressControlConfig{
		DefaultMaxConcurrent: 100,
		DefaultTimeout:       100 * time.Millisecond,
		RouteLimits: []RouteLimit{
			{Path: "/heavy", MaxConcurrent: 1, Timeout: 20 * time.Millisecond},
		},
	}
	mw := NewStressControlMiddleware(cfg)

	started := make(chan struct{})
	unblock := make(chan struct{})

	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-unblock
		w.WriteHeader(http.StatusOK)
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/heavy/task", nil))
	}()
	<-started

	rec := httptest.NewRecorder()
	mw(slowHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/heavy/task", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	close(unblock)
	wg.Wait()
}

func TestStressControl_DefaultUnaffectedByRouteSaturation(t *testing.T) {
	cfg := StressControlConfig{
		DefaultMaxConcurrent: 10,
		DefaultTimeout:       100 * time.Millisecond,
		RouteLimits: []RouteLimit{
			{Path: "/heavy", MaxConcurrent: 1, Timeout: 20 * time.Millisecond},
		},
	}
	mw := NewStressControlMiddleware(cfg)

	started := make(chan struct{})
	unblock := make(chan struct{})

	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-unblock
		w.WriteHeader(http.StatusOK)
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/heavy/task", nil))
	}()
	<-started

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	close(unblock)
	wg.Wait()
}

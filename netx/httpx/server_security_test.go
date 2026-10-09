package httpx

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
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
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
	assert.Empty(t, h.Get("Strict-Transport-Security"), "no HSTS over plain HTTP")
	assert.Contains(t, h.Get("Content-Security-Policy"), "default-src 'self'")
	assert.Contains(t, h.Get("Content-Security-Policy"), "frame-ancestors 'none'")
}

// Regression: empty Server and X-Powered-By headers were emitted.
func TestSecurityHeaders_NoEmptyFingerprintHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	SecurityHeaders(okHandler()).ServeHTTP(rec, req)

	_, server := rec.Header()["Server"]
	_, poweredBy := rec.Header()["X-Powered-By"]
	assert.False(t, server)
	assert.False(t, poweredBy)
}

func TestSecurityHeaders_HandlerOverridesCacheControl(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))
}

// Regression: HSTS always had preload and was sent over plain HTTP.
func TestSecurityHeaders_HSTS(t *testing.T) {
	tlsReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.TLS = &tls.ConnectionState{}
		return r
	}
	proxied := func(peer, proto string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = peer + ":1234"
		r.Header.Set("X-Forwarded-Proto", proto)
		return r
	}
	proxies := []string{"10.0.0.0/8"}
	cases := []struct {
		name string
		cfg  SecurityHeadersConfig
		req  *http.Request
		want string
	}{
		{"TLS default", SecurityHeadersConfig{}, tlsReq(), "max-age=63072000; includeSubDomains"},
		{"preload opt-in", SecurityHeadersConfig{HSTS: HSTSConfig{Preload: true}}, tlsReq(), "max-age=63072000; includeSubDomains; preload"},
		{"custom", SecurityHeadersConfig{HSTS: HSTSConfig{MaxAge: time.Hour, ExcludeSubDomains: true}}, tlsReq(), "max-age=3600"},
		{"disabled", SecurityHeadersConfig{HSTS: HSTSConfig{Disabled: true}}, tlsReq(), ""},
		{"trusted proxy https", SecurityHeadersConfig{TrustedProxies: proxies}, proxied("10.1.2.3", "https"), "max-age=63072000; includeSubDomains"},
		{"trusted proxy list", SecurityHeadersConfig{TrustedProxies: proxies}, proxied("10.1.2.3", "HTTPS, http"), "max-age=63072000; includeSubDomains"},
		{"trusted proxy http", SecurityHeadersConfig{TrustedProxies: proxies}, proxied("10.1.2.3", "http"), ""},
		{"untrusted peer", SecurityHeadersConfig{TrustedProxies: proxies}, proxied("203.0.113.9", "https"), ""},
		{"no proxies trusted", SecurityHeadersConfig{}, proxied("10.1.2.3", "https"), ""},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		SecurityHeadersWith(tc.cfg)(okHandler()).ServeHTTP(rec, tc.req)
		assert.Equal(t, tc.want, rec.Header().Get("Strict-Transport-Security"), tc.name)
	}
}

// Regression: 429/503 responses produced before SecurityHeaders lacked them.
func TestServer_SecurityHeadersOnEarlyRejections(t *testing.T) {
	ws := NewServer(&WSConfig{StressControl: &StressControlConfig{DefaultMaxConcurrent: 1, DefaultTimeout: time.Millisecond}}).(*httpServer)
	hold := make(chan struct{})
	ws.AddHandlers(routesFunc(func() []*Route {
		return PublicRoutes("/", GET("/slow").To(func(w http.ResponseWriter, _ *http.Request) { <-hold }))
	}))
	go serverRequest(ws.router, http.MethodGet, "/slow", nil)
	time.Sleep(20 * time.Millisecond)
	rec := serverRequest(ws.router, http.MethodGet, "/slow", nil)
	close(hold)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.NotEmpty(t, rec.Header().Get(RequestIDHeader))
}

// Regression: chi's Recoverer printed a pretty stack to stderr only.
func TestServer_RecovererLogsAndRespondsGeneric500(t *testing.T) {
	logs := captureLogs(t)

	ws := NewServer(&WSConfig{}).(*httpServer)
	ws.AddHandlers(routesFunc(func() []*Route {
		return PublicRoutes("/", GET("/boom").To(func(http.ResponseWriter, *http.Request) { panic("secret state") }))
	}))
	rec := serverRequest(ws.router, http.MethodGet, "/boom", map[string]string{RequestIDHeader: "req-42"})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":500,"message":"internal server error"}`, rec.Body.String())
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, logs.String(), `"msg":"panic recovered"`)
	assert.Contains(t, logs.String(), `"request_id":"req-42"`)
	assert.Contains(t, logs.String(), `"panic":"secret state"`)
	assert.Contains(t, logs.String(), `"stack":`)
}

func TestRecoverer_RepanicsAbortHandler(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
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

func TestLimitBody_RejectsOversizedContentLengthBeforeHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	req.ContentLength = DefaultMaxBodyBytes + 1
	rec := httptest.NewRecorder()

	called := false
	LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(rec, req)

	assert.False(t, called, "handler must not run for an oversized body")
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestLimitBody_CapsChunkedBodyOnRead(t *testing.T) {
	const overLimit = DefaultMaxBodyBytes + 1

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, overLimit)))
	req.ContentLength = -1 // chunked: no length announced upfront
	rec := httptest.NewRecorder()

	var readErr error
	LimitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})).ServeHTTP(rec, req)

	require.Error(t, readErr, "reading past the limit must produce an error")
}

// ── LimitBodyWithMax ──────────────────────────────────────────────────────────

func TestLimitBodyWithMax_HonorsConfiguredCap(t *testing.T) {
	const cap1KB = 1 << 10

	t.Run("body within the cap reaches the handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, cap1KB/2)))
		rec := httptest.NewRecorder()

		var readErr error
		LimitBodyWithMax(cap1KB)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, readErr = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(rec, req)

		require.NoError(t, readErr)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("body above the cap is rejected with 413", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, cap1KB+1)))
		rec := httptest.NewRecorder()

		LimitBodyWithMax(cap1KB)(okHandler()).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})
}

func TestLimitBodyWithMax_AcceptsBodyAboveTheDefaultCap(t *testing.T) {
	const cap32MB = 32 << 20

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	req.ContentLength = DefaultMaxBodyBytes + 1
	rec := httptest.NewRecorder()

	called := false
	LimitBodyWithMax(cap32MB)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	assert.True(t, called, "a cap above the default must let the body through")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLimitBodyWithMax_FallsBackToDefaultWhenNonPositive(t *testing.T) {
	for _, maxBody := range []int64{0, -1} {
		t.Run("cap falls back to the default", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			req.ContentLength = DefaultMaxBodyBytes + 1
			rec := httptest.NewRecorder()

			LimitBodyWithMax(maxBody)(okHandler()).ServeHTTP(rec, req)

			assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		})
	}
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
		wg.Go(func() {
			mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		})
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
	wg.Go(func() {
		mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/heavy/task", nil))
	})
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
	wg.Go(func() {
		mw(slowHandler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/heavy/task", nil))
	})
	<-started

	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	close(unblock)
	wg.Wait()
}

// ── Slow request bodies ───────────────────────────────────────────────────────

// startStressServer serves an echo route behind a 2-slot stress limiter.
func startStressServer(t *testing.T, bufferBytes int64) string {
	t.Helper()
	ws := NewServer(&WSConfig{StressControl: &StressControlConfig{
		DefaultMaxConcurrent: 2,
		DefaultTimeout:       50 * time.Millisecond,
		BufferBodyBytes:      bufferBytes,
	}}).(*httpServer)
	ws.AddHandlers(&mockRouterHandler{routes: PublicRoutes("/",
		POST("/echo").To(readBodyHandler),
		GET("/fast").To(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
	)})
	srv := httptest.NewServer(ws.handler())
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// openSlowBodies opens n connections that announce a 10-byte body and send
// only sent bytes of it, the way a slow-body attacker holds requests open.
func openSlowBodies(t *testing.T, addr string, n, sent int) {
	t.Helper()
	for range n {
		conn, err := net.Dial("tcp", addr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		_, err = fmt.Fprintf(conn, "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\n%s", strings.Repeat("x", sent))
		require.NoError(t, err)
	}
	time.Sleep(100 * time.Millisecond) // let the server start handling them
}

func TestStressControl_SlowBodiesDoNotStarveFastRequests(t *testing.T) {
	addr := startStressServer(t, 0)
	openSlowBodies(t, addr, 4, 5)

	resp, err := http.Get("http://" + addr + "/fast")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "slow bodies must not hold concurrency slots")

	resp, err = http.Post("http://"+addr+"/echo", "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStressControl_BodiesAboveBufferHoldSlots(t *testing.T) {
	// Control for the test above: once the body exceeds the buffer the slot is
	// taken while the rest trickles in, so the same attack saturates the pool.
	addr := startStressServer(t, 4)
	openSlowBodies(t, addr, 2, 5)

	resp, err := http.Get("http://" + addr + "/fast")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Equal(t, "1", resp.Header.Get("Retry-After"))
}

func TestBufferBody_PreservesBodyAndErrors(t *testing.T) {
	for _, size := range []int{1, 8, 9, 100} {
		body := strings.Repeat("a", size)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		bufferBody(req, 8)
		got, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, body, string(got), "size %d", size)
		require.NoError(t, req.Body.Close())
	}

	boom := errors.New("read timeout")
	req := httptest.NewRequest(http.MethodPost, "/", io.MultiReader(strings.NewReader("ab"), errReader{boom}))
	bufferBody(req, 8)
	got, err := io.ReadAll(req.Body)
	assert.Equal(t, "ab", string(got))
	assert.ErrorIs(t, err, boom, "a read error while buffering must reach the handler")
}

func TestDefaultMaxConcurrent_AtLeast256(t *testing.T) {
	assert.GreaterOrEqual(t, DefaultMaxConcurrent(), 256)
}

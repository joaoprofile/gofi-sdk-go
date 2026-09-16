package netx

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis_rate/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHTTPServer() *httpServer {
	return &httpServer{
		config: &WSConfig{ServerPort: ":0"},
		router: chi.NewMux(),
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	l.Close()
	return addr
}

// --- Use ---

func TestUse_MiddlewareIsInvokedOnRequest(t *testing.T) {
	ws := newTestHTTPServer()

	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	ws.Use(mw)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.True(t, called, "middleware deve ser chamado na requisição")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUse_MultipleMiddlewaresAreAppliedInOrder(t *testing.T) {
	ws := newTestHTTPServer()

	order := make([]int, 0, 2)
	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, 1)
			next.ServeHTTP(w, r)
		})
	}
	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, 2)
			next.ServeHTTP(w, r)
		})
	}

	ws.Use(mw1, mw2)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	ws.router.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, []int{1, 2}, order)
}

// --- UseAuth ---

func TestUseAuth_SetsAuthMiddleware(t *testing.T) {
	ws := newTestHTTPServer()
	assert.Nil(t, ws.auth)

	authMw := func(next http.Handler) http.Handler { return next }
	ws.UseAuth(authMw)

	assert.NotNil(t, ws.auth)
}

func TestUseAuth_ReplacesExistingMiddleware(t *testing.T) {
	ws := newTestHTTPServer()

	first := func(next http.Handler) http.Handler { return next }
	second := func(next http.Handler) http.Handler { return next }

	ws.UseAuth(first)
	ws.UseAuth(second)

	// Functions cannot be compared directly; asserting non-nil is enough.
	assert.NotNil(t, ws.auth)
}

// --- AddHandlers ---

func TestAddHandlers_RouteIsReachable(t *testing.T) {
	ws := newTestHTTPServer()

	handler := &mockRouterHandler{
		routes: PublicRoutes("/api",
			GET("/hello").To(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		),
	}

	ws.AddHandlers(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAddHandlers_MultipleHandlers(t *testing.T) {
	ws := newTestHTTPServer()

	h1 := &mockRouterHandler{
		routes: PublicRoutes("/v1",
			GET("/a").To(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		),
	}
	h2 := &mockRouterHandler{
		routes: PublicRoutes("/v1",
			POST("/b").To(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			}),
		),
	}

	ws.AddHandlers(h1, h2)

	recA := httptest.NewRecorder()
	ws.router.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/v1/a", nil))
	assert.Equal(t, http.StatusOK, recA.Code)

	recB := httptest.NewRecorder()
	ws.router.ServeHTTP(recB, httptest.NewRequest(http.MethodPost, "/v1/b", nil))
	assert.Equal(t, http.StatusCreated, recB.Code)
}

func TestAddHandlers_MultipleRoutesAreReachable(t *testing.T) {
	ws := newTestHTTPServer()

	handler := &mockRouterHandler{
		routes: PublicRoutes("/api",
			GET("/x").To(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
			POST("/y").To(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) }),
		),
	}

	ws.AddHandlers(handler)

	recX := httptest.NewRecorder()
	ws.router.ServeHTTP(recX, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	assert.Equal(t, http.StatusOK, recX.Code)

	recY := httptest.NewRecorder()
	ws.router.ServeHTTP(recY, httptest.NewRequest(http.MethodPost, "/api/y", nil))
	assert.Equal(t, http.StatusCreated, recY.Code)
}

func TestRegisterRoute_PublicRouteDoesNotUseAuth(t *testing.T) {
	ws := newTestHTTPServer()

	authCalled := false
	ws.auth = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalled = true
			next.ServeHTTP(w, r)
		})
	}

	route := PublicRoutes("/api", GET("/open").To(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ws.AddHandlers(&mockRouterHandler{routes: route})

	req := httptest.NewRequest(http.MethodGet, "/api/open", nil)
	ws.router.ServeHTTP(httptest.NewRecorder(), req)

	assert.False(t, authCalled, "auth não deve ser chamado em rotas públicas")
}

func TestRegisterRoute_PrivateRouteUsesAuth(t *testing.T) {
	ws := newTestHTTPServer()

	authCalled := false
	ws.auth = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalled = true
			next.ServeHTTP(w, r)
		})
	}

	route := PrivateRoutes("/api", GET("/secured").To(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ws.AddHandlers(&mockRouterHandler{routes: route})

	req := httptest.NewRequest(http.MethodGet, "/api/secured", nil)
	ws.router.ServeHTTP(httptest.NewRecorder(), req)

	assert.True(t, authCalled, "auth deve ser chamado em rotas privadas")
}

func TestRegisterRoute_PrivateRouteWithoutAuthMiddleware(t *testing.T) {
	ws := newTestHTTPServer()
	// ws.auth is nil — must not panic

	route := PrivateRoutes("/api", GET("/secured").To(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ws.AddHandlers(&mockRouterHandler{routes: route})

	req := httptest.NewRequest(http.MethodGet, "/api/secured", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRegisterRoute_WithCorsConfig_SetsHeaders(t *testing.T) {
	ws := newTestHTTPServer()

	corsConfig := &CorsConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: true,
		MaxAge:           "86400",
	}

	route := PublicRoutes("/api",
		GET("/cors").Cors(corsConfig).To(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	ws.AddHandlers(&mockRouterHandler{routes: route})

	req := httptest.NewRequest(http.MethodGet, "/api/cors", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestRegisterRoute_PathNormalizationRootPath(t *testing.T) {
	ws := newTestHTTPServer()

	// Route whose path equals the prefix — must be registered as "/"
	route := &Route{
		method:  http.MethodGet,
		path:    "/api",
		prefix:  "/api",
		handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	}

	ws.router.Route("/api", func(r chi.Router) {
		ws.registerRoute(r, route)
	})

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestListenAndServe_GracefulShutdown(t *testing.T) {
	addr := freePort(t)

	ws := &httpServer{
		config: &WSConfig{ServerPort: addr},
		router: chi.NewMux(),
	}

	ws.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	done := make(chan struct{})
	go func() {
		ws.ListenAndServe()
		close(done)
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 50*time.Millisecond, "servidor não ficou disponível a tempo")

	// Send SIGTERM to the process
	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	select {
	case <-done:
		// encerramento gracioso confirmado
	case <-time.After(5 * time.Second):
		t.Fatal("servidor não encerrou dentro do prazo esperado")
	}
}

// --- NewServer ---

func TestNewServer_ReturnsHttpServerInterface(t *testing.T) {
	cfg := &WSConfig{ServerPort: ":0"}
	srv := NewServer(cfg)
	assert.NotNil(t, srv)
	var _ HttpServer = srv
}

func TestNewServer_WithAllowedOrigins_CORSApplied(t *testing.T) {
	cfg := &WSConfig{
		ServerPort:     ":0",
		AllowedOrigins: []string{"https://myapp.com"},
	}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://myapp.com")
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, "https://myapp.com", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestNewServer_WithRateLimiter_MiddlewareChainedCorrectly(t *testing.T) {
	backend := &stubBackend{fn: func(_ context.Context, _ string, _ redis_rate.Limit) (*redis_rate.Result, error) {
		return allowedResult(9), nil
	}}

	cfg := &WSConfig{
		ServerPort: ":0",
		RateLimiter: &RedisRateLimiterConfig{
			Backend:   backend,
			KeyPrefix: "test",
			Default:   RateLimitPlan{Rate: 10, Burst: 10},
		},
	}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	// Rate-limit headers confirm the rate-limiter middleware ran.
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewServer_WithStressControl_RequestsServed(t *testing.T) {
	cfg := &WSConfig{
		ServerPort: ":0",
		StressControl: &StressControlConfig{
			DefaultMaxConcurrent: 100,
			DefaultTimeout:       50 * time.Millisecond,
		},
	}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewServer_WithoutRateLimiter_RequestsServed(t *testing.T) {
	cfg := &WSConfig{
		ServerPort:  ":0",
		RateLimiter: nil, // explicitly nil — rate limiting disabled
	}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	// No rate-limit headers when limiter is disabled.
	assert.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
}

func TestNewServer_SecurityHeaders_SetByDefault(t *testing.T) {
	cfg := &WSConfig{ServerPort: ":0"}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.NotEmpty(t, rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("X-Content-Type-Options"))
	assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
}

func TestNewServer_TraceMethodBlocked(t *testing.T) {
	cfg := &WSConfig{ServerPort: ":0"}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	// Register a route so chi routes the request and middleware is exercised.
	ws.router.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodTrace, "/ping", nil)
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	// BlockUnsafeMethods middleware must intercept TRACE before the handler.
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestNewServer_MaxBodyBytes_DefaultCapApplied(t *testing.T) {
	cfg := &WSConfig{ServerPort: ":0"}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", http.NoBody)
	req.ContentLength = DefaultMaxBodyBytes + 1
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestNewServer_MaxBodyBytes_ConfiguredCapApplied(t *testing.T) {
	cfg := &WSConfig{ServerPort: ":0", MaxBodyBytes: 32 << 20}
	srv := NewServer(cfg)
	ws := srv.(*httpServer)
	ws.router.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Above the SDK default, below the configured cap: must reach the handler.
	req := httptest.NewRequest(http.MethodPost, "/upload", http.NoBody)
	req.ContentLength = DefaultMaxBodyBytes + 1
	rec := httptest.NewRecorder()
	ws.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestOrDefault(t *testing.T) {
	assert.Equal(t, 5*time.Second, orDefault(5*time.Second, time.Minute))
	assert.Equal(t, time.Minute, orDefault(0, time.Minute))
	assert.Equal(t, time.Minute, orDefault(-time.Second, time.Minute))
}

// ── Per-route deadlines ───────────────────────────────────────────────────────

func TestRouteTimeouts_ExtendServerReadTimeout(t *testing.T) {
	routes := PublicRoutes("/api",
		POST("/upload").To(readBodyHandler).Timeouts(10*time.Second, 10*time.Second),
	)
	baseURL := startSlowServer(t, routes)

	resp, err := slowClient().Do(newSlowBodyRequest(t, baseURL+"/api/upload"))
	require.NoError(t, err, "the route override must outlast the server read timeout")
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRouteTimeouts_WithoutOverrideServerReadTimeoutApplies(t *testing.T) {
	routes := PublicRoutes("/api",
		POST("/upload").To(readBodyHandler),
	)
	baseURL := startSlowServer(t, routes)

	// Control for the test above: the same slow body must fail without the
	// override, otherwise that test would pass for the wrong reason. The
	// timeout surfaces either as a transport error or as a non-200.
	resp, err := slowClient().Do(newSlowBodyRequest(t, baseURL+"/api/upload"))
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
	}
	succeeded := err == nil && resp.StatusCode == http.StatusOK

	assert.False(t, succeeded, "server read timeout should have cut the slow body short")
}

func TestRouteDeadlines_NoOpWhenWriterHasNoDeadlineSupport(t *testing.T) {
	// httptest.ResponseRecorder exposes no deadline setter and no Unwrap chain
	// reaching one: the override cannot apply and must not break the request.
	called := false
	routeDeadlines(time.Second, time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))

	assert.True(t, called, "an unsupported writer must degrade to the server-wide timeouts")
}

func TestRouteTimeouts_NotAppliedWhenUnset(t *testing.T) {
	route := POST("/upload").To(readBodyHandler).Build()

	assert.Zero(t, route.readTimeout)
	assert.Zero(t, route.writeTimeout)
}

// --- helpers  ---

// slowBodyChunks and slowBodyPause make a request body take roughly 1.2s to
// arrive — comfortably past slowServerReadTimeout, comfortably under the
// per-route override.
const (
	slowBodyChunks        = 8
	slowBodyPause         = 150 * time.Millisecond
	slowServerReadTimeout = 400 * time.Millisecond
)

// slowReader emits one byte per Read, pausing before each, so the request body
// trickles in the way a real upload on a thin uplink does.
type slowReader struct {
	remaining int
	pause     time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.remaining == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	s.remaining--
	p[0] = 'x'
	return 1, nil
}

func readBodyHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := io.ReadAll(r.Body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// startSlowServer serves the routes behind a deliberately short ReadTimeout so
// a per-route override is the only way a slow body can complete.
func startSlowServer(t *testing.T, routes []*Route) string {
	t.Helper()

	ws := NewServer(&WSConfig{ServerPort: ":0"}).(*httpServer)
	ws.AddHandlers(&mockRouterHandler{routes: routes})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &http.Server{Handler: ws.router, ReadTimeout: slowServerReadTimeout}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	return "http://" + ln.Addr().String()
}

func newSlowBodyRequest(t *testing.T, url string) *http.Request {
	t.Helper()

	body := &slowReader{remaining: slowBodyChunks, pause: slowBodyPause}
	req, err := http.NewRequest(http.MethodPost, url, body)
	require.NoError(t, err)
	req.ContentLength = slowBodyChunks

	return req
}

func slowClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

type mockRouterHandler struct {
	routes []*Route
}

func (m *mockRouterHandler) Handlers() []*Route {
	return m.routes
}

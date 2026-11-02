package httpx

import (
	"context"
	"errors"
	"fmt"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// Server timeouts applied when the matching WSConfig field is <= 0.
const (
	DefaultReadTimeout = 10 * time.Second
	// DefaultWriteTimeout exceeds DefaultRequestTimeout so handlers that use their
	// whole budget can still write the response.
	DefaultWriteTimeout   = 35 * time.Second
	DefaultIdleTimeout    = 60 * time.Second
	DefaultRequestTimeout = 30 * time.Second
	// DefaultShutdownTimeout stays below Kubernetes' default 30s grace period.
	DefaultShutdownTimeout = 25 * time.Second
	// DefaultDrainDelay is applied when Health is enabled, so endpoints observe
	// the failed readiness before connections close.
	DefaultDrainDelay = 5 * time.Second

	// readHeaderTimeout is not configurable: it is the Slowloris defense, and
	// it must stay short even when a service relaxes ReadTimeout to accept
	// large uploads.
	readHeaderTimeout = 5 * time.Second
)

// orDefault returns fallback when d is not a positive duration.
func orDefault(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

type HttpServer interface {
	Use(middleware ...Middleware)
	UseAuth(authMiddleware Middleware)
	AddHandlers(handlers ...RouterHandler)
	// ListenAndServe serves until SIGINT/SIGTERM or Shutdown, then fails
	// readiness, drains and stops gracefully. It returns nil on a clean stop.
	ListenAndServe() error
	// Shutdown stops a running ListenAndServe and waits for it to finish, or
	// for ctx. Calling it before ListenAndServe makes ListenAndServe return.
	Shutdown(ctx context.Context) error
	// AddHealthCheck registers a readiness check (requires WSConfig.Health).
	AddHealthCheck(name string, check func(ctx context.Context) error)
}

type httpServer struct {
	config *WSConfig
	auth   Middleware
	router *chi.Mux
	stress Middleware // concurrency limiter applied per route; nil disables it

	cors       *corsPolicy                 // global CORS policy
	preflights *routePreflights            // preflights of routes with their own CORS
	csrf       *http.CrossOriginProtection // nil when disabled

	ready     atomic.Bool
	checksMu  sync.RWMutex
	checks    map[string]func(ctx context.Context) error
	readiness readinessCache

	lifeMu sync.Mutex
	stop   context.CancelFunc // cancels the running ListenAndServe
	done   chan struct{}      // closed when ListenAndServe returns
	closed bool               // Shutdown was requested
}

// NewServer builds the server. It panics on an invalid config; call
// WSConfig.Validate first to get an error instead.
func NewServer(config *WSConfig) HttpServer {
	if err := config.Validate(); err != nil {
		panic(err)
	}
	mux := chi.NewMux()
	global := mustCORSPolicy(config.corsConfig())
	ws := &httpServer{
		config:     config,
		router:     mux,
		cors:       global,
		preflights: newRoutePreflights(global),
	}
	if !config.DisableCrossOriginProtection {
		ws.csrf = crossOriginProtection(global.trusted)
	}

	// Request ID first, so every log line, the panic handler included, has it.
	// Security headers come next, so 429/503/preflight responses carry them.
	mux.Use(requestContext(config.ExposeErrorCause))
	mux.Use(SecurityHeadersWith(SecurityHeadersConfig{HSTS: config.HSTS, TrustedProxies: config.TrustedProxies}))
	mux.Use(Recoverer)

	// Resolve the client IP first so the rate limiter and logs use the same value.
	mux.Use(ClientIPMiddleware(config.TrustedProxies))

	// Global CORS; a route policy replaces it (registerRoute), preflights included.
	mux.Use(corsMiddleware(global, ws.preflights.lookup))

	// Rate limiter — applied only when the caller explicitly provides a config.
	// Redis is never created internally; the caller is responsible for wiring
	// the backend via NewRedisBackend or a custom RateLimiterBackend.
	if config.RateLimiter != nil {
		mux.Use(NewRedisRateLimiter(*config.RateLimiter))
	}

	// Structured JSON request logging
	mux.Use(LoggingMiddleware())

	// Method blocking and body limit.
	mux.Use(BlockUnsafeMethods)
	mux.Use(LimitBodyWithMax(config.MaxBodyBytes))

	// Concurrency control wraps each route (see registerRoute), so the slot is
	// taken after routing, rate limiting and the body-size check, and after a
	// route's extended read deadline is in place for body buffering.
	// Health probes are served ahead of the router, so they never wait here.
	var stressConfig StressControlConfig
	if config.StressControl != nil {
		stressConfig = *config.StressControl
	}
	ws.stress = NewStressControlMiddleware(stressConfig)
	return ws
}

func (ws *httpServer) Use(middlewares ...Middleware) {
	for _, m := range middlewares {
		ws.router.Use(m)
	}
}

func (ws *httpServer) UseAuth(authMiddleware Middleware) { ws.auth = authMiddleware }

// AddHandlers registers every route on the root router under its full pattern.
// Groups whose prefixes nest ("/orders" and "/orders/{id}") stay independent:
// a sub-router per prefix would let the deeper one shadow the other's routes.
func (ws *httpServer) AddHandlers(handlers ...RouterHandler) {
	for _, handlerGroup := range handlers {
		for _, route := range handlerGroup.Handlers() {
			ws.registerRoute(route)
		}
	}
}

// routeDeadlines extends the connection read/write deadlines for a single
// route, overriding Server.ReadTimeout and Server.WriteTimeout. Connection
// deadlines are the only budget a route can lengthen: context deadlines
// (RequestTimeout) can be tightened downstream but never relaxed.
//
// When the ResponseWriter exposes no deadline setter — directly or through an
// Unwrap chain — the override cannot be applied and the route silently keeps
// the server-wide timeouts. That is a permanent property of the writer, so it
// is reported once instead of on every request.
func routeDeadlines(read, write time.Duration) Middleware {
	var warnOnce sync.Once

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := http.NewResponseController(w)
			now := time.Now()

			var err error
			if read > 0 {
				err = rc.SetReadDeadline(now.Add(read))
			}
			if write > 0 && err == nil {
				err = rc.SetWriteDeadline(now.Add(write))
			}

			if err != nil {
				warnOnce.Do(func() {
					logging.FromContext(r.Context()).Warn("route deadlines not applied",
						slog.String("path", r.URL.Path),
						slog.Any("error", err),
					)
				})
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (ws *httpServer) registerRoute(route *Route) {
	var h http.Handler = http.HandlerFunc(route.handler)

	// A root prefix ("/") would strip the leading slash chi requires.
	path := strings.TrimPrefix(route.path, route.prefix)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	pattern := routePattern(route.prefix, path)

	// Auth: only applied to private routes when an auth middleware is registered.
	if route.authentication && ws.auth != nil {
		h = ws.auth(h)
	}

	// CSRF and the route's CORS policy run before auth.
	h = ws.crossOrigin(route, path, pattern, h)

	// The concurrency slot covers auth and the handler.
	if ws.stress != nil {
		h = ws.stress(h)
	}

	// The handler budget is per route (RouteBuilder.RequestTimeout, else
	// WSConfig.RequestTimeout) and covers the wait for a slot, auth and the handler.
	h = middleware.Timeout(orDefault(route.requestTimeout, orDefault(ws.config.RequestTimeout, DefaultRequestTimeout)))(h)

	// Per-route deadlines wrap outermost, so the extended budget is in place
	// before the stress limiter buffers, or auth or the handler reads, the body.
	if read, write := route.readTimeout, ws.routeWriteTimeout(route); read > 0 || write > 0 {
		h = routeDeadlines(read, write)(h)
	}

	// Outermost, so requests rejected by auth or deadlines are labeled too.
	h = routeTelemetry(route.method, pattern)(h)

	ws.router.Method(route.method, pattern, h)
	if path == "/" && pattern != "/" {
		ws.router.Method(route.method, pattern+"/", h) // a group root answers with and without the trailing slash
	}
}

// routeWriteTimeout is the route's write deadline override. A route whose
// RequestTimeout outgrows the server-wide WriteTimeout gets a write deadline
// that keeps the default margin, so a handler using its whole budget can still
// write the response; an explicit Timeouts write value is kept when larger.
func (ws *httpServer) routeWriteTimeout(route *Route) time.Duration {
	if route.requestTimeout <= 0 {
		return route.writeTimeout
	}
	needed := route.requestTimeout + (DefaultWriteTimeout - DefaultRequestTimeout)
	if needed <= orDefault(ws.config.WriteTimeout, DefaultWriteTimeout) || needed <= route.writeTimeout {
		return route.writeTimeout
	}
	return needed
}

// crossOrigin wraps h with the CSRF check and, when the route has its own
// CORS policy, with that policy, which replaces the global one (preflights
// included) and alone sets the origins trusted by the CSRF check. A route
// policy with AnyOrigin skips the CSRF check.
func (ws *httpServer) crossOrigin(route *Route, path, pattern string, h http.Handler) http.Handler {
	if route.corsConfig == nil {
		if ws.csrf != nil {
			h = ws.csrf.Handler(h)
		}
		return h
	}
	policy := mustCORSPolicy(*route.corsConfig)
	ws.preflights.add(pattern, route.method, policy)
	if path == "/" && pattern != "/" {
		ws.preflights.add(pattern+"/", route.method, policy) // chi serves both
	}
	// A route open to any origin carries no credentials (CorsConfig.Validate forbids
	// both), so a cross-site request has no ambient authority to abuse: it is the
	// route's purpose (an embedded widget, a public API) and needs no CSRF check.
	if ws.csrf != nil && !policy.anyOrigin {
		h = crossOriginProtection(policy.trusted).Handler(h)
	}
	return routeCORS(policy)(h)
}

// routePattern joins prefix and path the way chi reports them: the root of a
// group is the prefix itself ("/orders", not "/orders/").
func routePattern(prefix, path string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if path == "/" && prefix != "" {
		return prefix
	}
	return prefix + path
}

func (ws *httpServer) ListenAndServe() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ws.lifeMu.Lock()
	if ws.closed {
		ws.lifeMu.Unlock()
		return nil
	}
	ws.stop, ws.done = stop, make(chan struct{})
	done := ws.done
	ws.lifeMu.Unlock()
	defer close(done)

	return ws.serve(ctx)
}

func (ws *httpServer) Shutdown(ctx context.Context) error {
	ws.lifeMu.Lock()
	ws.closed = true
	stop, done := ws.stop, ws.done
	ws.lifeMu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serve runs until ctx is done, then fails readiness, waits DrainDelay and shuts
// down gracefully within ShutdownTimeout.
func (ws *httpServer) serve(ctx context.Context) error {
	if ws.config.H2C && ws.config.TLS != nil {
		return errors.New("httpx: H2C and TLS are mutually exclusive (TLS already negotiates HTTP/2)")
	}
	tlsConfig, err := netx.ServerTLSConfig(ws.config.TLS)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", ws.config.ServerPort)
	if err != nil {
		return fmt.Errorf("httpx: listen %s: %w", ws.config.ServerPort, err)
	}

	baseCtx, cancelBase := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelBase()

	server := &http.Server{
		Handler:           ws.instrumented(ws.handler()),
		ReadTimeout:       orDefault(ws.config.ReadTimeout, DefaultReadTimeout),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      orDefault(ws.config.WriteTimeout, DefaultWriteTimeout),
		IdleTimeout:       orDefault(ws.config.IdleTimeout, DefaultIdleTimeout),
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		TLSConfig:         tlsConfig,
	}
	if ws.config.H2C {
		var p http.Protocols
		p.SetHTTP1(true)
		p.SetUnencryptedHTTP2(true)
		server.Protocols = &p
	}

	serveErr := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			serveErr <- server.ServeTLS(ln, "", "")
			return
		}
		serveErr <- server.Serve(ln)
	}()
	ws.ready.Store(true)
	slog.InfoContext(ctx, "HTTP service started", slog.String("addr", ln.Addr().String()), slog.Bool("tls", tlsConfig != nil))

	select {
	case err := <-serveErr:
		ws.ready.Store(false)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("httpx: serve: %w", err)
	case <-ctx.Done():
	}

	// Fail readiness first so load balancers stop routing before connections close.
	ws.ready.Store(false)
	if d := ws.drainDelay(); d > 0 {
		slog.Info("HTTP service draining", slog.Duration("delay", d))
		time.Sleep(d)
	}

	shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orDefault(ws.config.ShutdownTimeout, DefaultShutdownTimeout))
	defer cancel()
	err = server.Shutdown(shCtx)
	cancelBase() // cancel in-flight handler contexts left after the deadline
	if err != nil {
		// Force-close the connections still open after the deadline.
		return fmt.Errorf("httpx: shutdown: %w", errors.Join(err, server.Close()))
	}
	slog.Info("HTTP service stopped gracefully")
	return nil
}

func (ws *httpServer) drainDelay() time.Duration {
	if ws.config.DrainDelay > 0 {
		return ws.config.DrainDelay
	}
	if ws.config.Health != nil {
		return DefaultDrainDelay
	}
	return 0
}

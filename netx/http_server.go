package netx

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/joaoprofile/gofi/obs/logging"
)

// Server timeouts applied when the matching WSConfig field is <= 0.
const (
	DefaultReadTimeout    = 10 * time.Second
	DefaultWriteTimeout   = 15 * time.Second
	DefaultIdleTimeout    = 60 * time.Second
	DefaultRequestTimeout = 30 * time.Second

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
	ListenAndServe()
}

type httpServer struct {
	config *WSConfig
	auth   Middleware
	router *chi.Mux
}

func NewServer(config *WSConfig) HttpServer {
	mux := chi.NewMux()

	// CORS — global config, can be overridden per-route via corsConfig on RouteBuilder.
	corsConfig := DefaultCORSConfig()
	if len(config.AllowedOrigins) > 0 {
		corsConfig.AllowedOrigins = config.AllowedOrigins
	}
	mux.Use(CORSMiddleware(corsConfig))

	// Rate limiter — applied only when the caller explicitly provides a config.
	// Redis is never created internally; the caller is responsible for wiring
	// the backend via NewRedisBackend or a custom RateLimiterBackend.
	if config.RateLimiter != nil {
		mux.Use(NewRedisRateLimiter(*config.RateLimiter))
	}

	// chi built-ins: request ID propagation, real IP extraction, panic recovery.
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Recoverer)

	// Structured JSON request logging
	mux.Use(LoggingMiddleware())

	// Concurrency control — use caller-supplied config or fall back to defaults.
	stressConfig := StressControlConfig{
		DefaultMaxConcurrent: 50,
		DefaultTimeout:       20 * time.Millisecond,
	}
	if config.StressControl != nil {
		stressConfig = *config.StressControl
	}
	mux.Use(NewStressControlMiddleware(stressConfig))

	// Global request timeout.
	mux.Use(middleware.Timeout(orDefault(config.RequestTimeout, DefaultRequestTimeout)))

	// Security hardening (headers, method blocking, body limit).
	mux.Use(BlockUnsafeMethods)
	mux.Use(SecurityHeaders)
	mux.Use(LimitBodyWithMax(config.MaxBodyBytes))

	return &httpServer{
		config: config,
		router: mux,
	}
}

func (ws *httpServer) Use(middlewares ...Middleware) {
	for _, m := range middlewares {
		ws.router.Use(m)
	}
}

func (ws *httpServer) UseAuth(authMiddleware Middleware) { ws.auth = authMiddleware }

func (ws *httpServer) AddHandlers(handlers ...RouterHandler) {
	grouped := make(map[string][]*Route)

	for _, handlerGroup := range handlers {
		for _, route := range handlerGroup.Handlers() {
			grouped[route.prefix] = append(grouped[route.prefix], route)
		}
	}

	for prefix, routes := range grouped {
		ws.router.Route(prefix, func(r chi.Router) {
			for _, route := range routes {
				ws.registerRoute(r, route)
			}
		})
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

func (ws *httpServer) registerRoute(r chi.Router, route *Route) {
	var h http.Handler = http.HandlerFunc(route.handler)

	// Per-route CORS: applies a route-specific CORS policy on top of the global
	// one. The route-level middleware runs closer to the handler, so its
	// Set() calls override the global headers for non-OPTIONS responses.
	if route.corsConfig != nil {
		h = CORSMiddleware(*route.corsConfig)(h)
	}

	// Auth: only applied to private routes when an auth middleware is registered.
	if route.authentication && ws.auth != nil {
		h = ws.auth(h)
	}

	// Per-route deadlines wrap outermost, so the extended budget is in place
	// before auth or the handler touches the request body.
	if route.readTimeout > 0 || route.writeTimeout > 0 {
		h = routeDeadlines(route.readTimeout, route.writeTimeout)(h)
	}

	path := strings.TrimPrefix(route.path, route.prefix)
	if path == "" {
		path = "/"
	}

	r.Method(route.method, path, h)
}

func (ws *httpServer) ListenAndServe() {
	server := &http.Server{
		Addr:              ws.config.ServerPort,
		Handler:           ws.router,
		ReadTimeout:       orDefault(ws.config.ReadTimeout, DefaultReadTimeout),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      orDefault(ws.config.WriteTimeout, DefaultWriteTimeout),
		IdleTimeout:       orDefault(ws.config.IdleTimeout, DefaultIdleTimeout),
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(os.Stderr, "http-server: ", log.LstdFlags),
		BaseContext: func(_ net.Listener) context.Context {
			return context.Background()
		},
	}

	idleConnsClosed := make(chan struct{})

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		log.Println("shutdown signal received")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}

		close(idleConnsClosed)
	}()

	log.Printf("HTTP service started at %s", ws.config.ServerPort)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}

	<-idleConnsClosed

	log.Println("HTTP service stopped gracefully")
}

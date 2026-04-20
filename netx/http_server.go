package netx

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

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
	mux.Use(middleware.Timeout(30 * time.Second))

	// Security hardening (headers, method blocking, body limit).
	mux.Use(BlockUnsafeMethods)
	mux.Use(SecurityHeaders)
	mux.Use(LimitBody)

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
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
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

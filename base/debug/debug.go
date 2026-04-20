package debug

import (
	"errors"
	"log/slog"
	"net/http"
	_ "net/http/pprof"

	"github.com/joaoprofile/gofi/base/environment"
)

// pprofRoutes are the standard net/http/pprof endpoints registered on
// http.DefaultServeMux by the blank import above.
var pprofRoutes = []string{
	"/debug/pprof/",
	"/debug/pprof/cmdline",
	"/debug/pprof/profile",
	"/debug/pprof/symbol",
	"/debug/pprof/trace",
}

// Config holds the configuration for the pprof debug server.
type Config struct {
	// Addr is the TCP address to listen on (e.g. "localhost:6060").
	Addr string
	// User and Pass, when both non-empty, protect every pprof route with
	// HTTP basic auth.
	User string
	Pass string
}

// Server is a pprof debug HTTP server with optional basic-auth protection.
type Server struct {
	cfg            Config
	listenAndServe func(addr string, handler http.Handler) error
}

// New returns a new debug Server with the given configuration.
func New(cfg Config) *Server {
	return &Server{
		cfg:            cfg,
		listenAndServe: http.ListenAndServe,
	}
}

// Handler builds the http.Handler for the debug server.
//
// When both User and Pass are set, every pprof route is wrapped by an HTTP
// basic-auth middleware before being forwarded to http.DefaultServeMux.
// Otherwise http.DefaultServeMux is returned directly (no auth).
func (s *Server) Handler() http.Handler {
	if s.cfg.User == "" || s.cfg.Pass == "" {
		return http.DefaultServeMux
	}

	mux := http.NewServeMux()
	wrap := func(h http.Handler) http.Handler {
		return basicAuthMiddleware(s.cfg.User, s.cfg.Pass, h)
	}

	for _, route := range pprofRoutes {
		mux.Handle(route, wrap(http.DefaultServeMux))
	}

	return mux
}

// ListenAndServe starts the debug HTTP server and blocks until it returns.
func (s *Server) ListenAndServe() error {
	slog.Info("debug server started", slog.String("addr", s.cfg.Addr))
	return s.listenAndServe(s.cfg.Addr, s.Handler())
}

// run starts the server and suppresses http.ErrServerClosed (the expected
// error on graceful shutdown). Any other error is logged and returned.
func (s *Server) run() error {
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("debug server error", slog.Any("error", err))
		return err
	}
	return nil
}

// Start reads the service configuration from the environment and, when the
// debug flag is enabled, starts the pprof server in a background goroutine.
func Start() {
	env := environment.Instance()
	if !env.ServiceDebug {
		return
	}

	srv := New(Config{
		Addr: env.ServiceDebugAddr,
		User: env.ServiceDebugUser,
		Pass: env.ServiceDebugPass, // was incorrectly set to ServiceDebugUser
	})

	go srv.run() //nolint:errcheck
}

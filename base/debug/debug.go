package debug

import (
	"errors"
	"expvar"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/redact"
)

// DefaultAddr is used when Config.Addr is empty; loopback only.
const DefaultAddr = "localhost:6060"

// Config holds the configuration for the pprof debug server.
type Config struct {
	// Addr is the TCP address to listen on. Defaults to DefaultAddr.
	Addr string

	// User and Pass, when both non-empty, protect every route with HTTP basic
	// auth. Without them a non-loopback Addr is rebound to 127.0.0.1.
	User string
	Pass string `redact:"true"`
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c Config) String() string                { return redact.Sprint(c) }
func (c Config) GoString() string              { return redact.GoSprint(c) }
func (c Config) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c Config) LogValue() slog.Value          { return redact.LogValue(c) }
func (c Config) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

// Server is a pprof debug HTTP server with optional basic-auth protection.
type Server struct {
	cfg   Config
	serve func(*http.Server) error
}

// Format keeps fmt from printing the unexported cfg (and its password) raw.
func (s *Server) Format(f fmt.State, _ rune) { _, _ = fmt.Fprintf(f, "debug.Server{cfg:%+v}", s.cfg) }

// New returns a new debug Server with the given configuration.
func New(cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = DefaultAddr
	}
	return &Server{
		cfg:   cfg,
		serve: (*http.Server).ListenAndServe,
	}
}

// Handler serves the pprof and expvar routes, behind basic auth when credentials are set.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/vars", expvar.Handler())

	if !s.hasAuth() {
		return mux
	}
	return basicAuthMiddleware(s.cfg.User, s.cfg.Pass, mux)
}

func (s *Server) hasAuth() bool { return s.cfg.User != "" && s.cfg.Pass != "" }

// ListenAndServe starts the debug HTTP server and blocks until it returns.
func (s *Server) ListenAndServe() error {
	addr := s.cfg.Addr
	if !s.hasAuth() && !isLoopback(addr) {
		addr = loopbackAddr(addr)
		slog.Warn("debug server has no credentials; listening on loopback only",
			slog.String("configured", s.cfg.Addr), slog.String("addr", addr))
	}
	slog.Info("debug server started", slog.String("addr", addr))
	// No WriteTimeout: profile and trace stream for the requested duration.
	return s.serve(&http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	})
}

func loopbackAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return DefaultAddr
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// Start launches the pprof server in a background goroutine using cfg. The
// caller decides whether debugging is enabled; gofi's config.StartDebug reads
// SERVICE_DEBUG and the SERVICE_DEBUG_* settings from the environment.
func Start(cfg Config) {
	go New(cfg).run() //nolint:errcheck
}

package debug

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// basicAuthMiddleware
// ---------------------------------------------------------------------------

func TestBasicAuthMiddleware_NoCredentials(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := basicAuthMiddleware("user", "pass", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Authorization header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header to be set")
	}
}

func TestBasicAuthMiddleware_WrongCredentials(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := basicAuthMiddleware("user", "pass", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("wrong", "credentials")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestBasicAuthMiddleware_ValidCredentials(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := basicAuthMiddleware("user", "pass", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("user", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
}

// ---------------------------------------------------------------------------
// Server.Handler
// ---------------------------------------------------------------------------

func TestServerHandler_NoAuthServesOnlyPprof(t *testing.T) {
	http.HandleFunc("/debug-test-private", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := New(Config{}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug-test-private", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("routes from http.DefaultServeMux must not be exposed, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected pprof index, got %d", rec.Code)
	}
}

func TestServerHandler_WithAuth(t *testing.T) {
	h := New(Config{User: "admin", Pass: "secret"}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without credentials, got %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with valid credentials, got %d", rec.Code)
	}
}

func TestListenAndServe_AddrPolicy(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"default addr", Config{}, DefaultAddr},
		{"explicit loopback", Config{Addr: "127.0.0.1:7000"}, "127.0.0.1:7000"},
		{"ipv6 loopback", Config{Addr: "[::1]:7000"}, "[::1]:7000"},
		{"all interfaces without auth", Config{Addr: ":7000"}, "127.0.0.1:7000"},
		{"public ip without auth", Config{Addr: "0.0.0.0:7000"}, "127.0.0.1:7000"},
		{"only user set", Config{Addr: ":7000", User: "admin"}, "127.0.0.1:7000"},
		{"all interfaces with auth", Config{Addr: ":7000", User: "admin", Pass: "secret"}, ":7000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(tt.cfg)
			var got string
			srv.serve = func(s *http.Server) error { got = s.Addr; return nil }
			if err := srv.ListenAndServe(); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("listen addr=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerHandler_ExposesExpvar(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Config{}).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/vars", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("expected /debug/vars, got %d", rec.Code)
	}
}

func TestListenAndServe_ServerHasTimeouts(t *testing.T) {
	srv := New(Config{})
	srv.serve = func(s *http.Server) error {
		if s.ReadHeaderTimeout <= 0 || s.IdleTimeout <= 0 {
			t.Errorf("missing timeouts: %+v", s)
		}
		if s.Addr != DefaultAddr {
			t.Errorf("Addr=%q, want %q", s.Addr, DefaultAddr)
		}
		return nil
	}
	if err := srv.ListenAndServe(); err != nil {
		t.Fatal(err)
	}
}

func TestServerRun(t *testing.T) {
	other := errors.New("unexpected listen error")
	for name, tc := range map[string]struct {
		serveErr error
		want     error
	}{
		"success":         {nil, nil},
		"ErrServerClosed": {http.ErrServerClosed, nil},
		"other error":     {other, other},
	} {
		t.Run(name, func(t *testing.T) {
			srv := New(Config{})
			srv.serve = func(*http.Server) error { return tc.serveErr }
			if err := srv.run(); !errors.Is(err, tc.want) {
				t.Errorf("run()=%v, want %v", err, tc.want)
			}
		})
	}
}

// Start (package-level) is exercised in gofi's config package, where the
// SERVICE_DEBUG env wiring lives (config.StartDebug).

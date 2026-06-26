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

func TestServerHandler_NoAuth(t *testing.T) {
	srv := New(Config{Addr: ":9999"})
	h := srv.Handler()

	// Without credentials, Handler must return http.DefaultServeMux directly.
	if h != http.DefaultServeMux {
		t.Error("expected http.DefaultServeMux when no credentials are set")
	}
}

func TestServerHandler_OnlyUser(t *testing.T) {
	srv := New(Config{Addr: ":9999", User: "admin"}) // Pass is empty
	h := srv.Handler()

	// Both User AND Pass must be non-empty; if either is missing, no auth.
	if h != http.DefaultServeMux {
		t.Error("expected http.DefaultServeMux when only User is set (Pass is empty)")
	}
}

func TestServerHandler_WithAuth(t *testing.T) {
	srv := New(Config{Addr: ":9999", User: "admin", Pass: "secret"})
	h := srv.Handler()

	// With credentials, a custom mux (not DefaultServeMux) must be returned.
	if h == http.DefaultServeMux {
		t.Error("expected a custom mux (not DefaultServeMux) when credentials are set")
	}

	// Verify the custom mux enforces auth on a pprof route.
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on pprof route without credentials, got %d", rec.Code)
	}

	// With valid credentials the mux should forward to DefaultServeMux (pprof).
	req2 := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req2.SetBasicAuth("admin", "secret")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	// pprof is registered, so we expect a successful response (200 or 303).
	if rec2.Code == http.StatusUnauthorized {
		t.Errorf("expected a non-401 response with valid credentials, got %d", rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// Server.ListenAndServe
// ---------------------------------------------------------------------------

func TestServerListenAndServe_Success(t *testing.T) {
	srv := New(Config{Addr: ":9999"})
	srv.listenAndServe = func(addr string, h http.Handler) error {
		if addr != ":9999" {
			t.Errorf("expected addr=\":9999\", got %q", addr)
		}
		return nil
	}

	if err := srv.ListenAndServe(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestServerListenAndServe_Error(t *testing.T) {
	want := errors.New("bind: address already in use")
	srv := New(Config{Addr: ":9999"})
	srv.listenAndServe = func(_ string, _ http.Handler) error { return want }

	err := srv.ListenAndServe()
	if !errors.Is(err, want) {
		t.Errorf("expected error %v, got %v", want, err)
	}
}

// ---------------------------------------------------------------------------
// Server.run
// ---------------------------------------------------------------------------

func TestServerRun_Success(t *testing.T) {
	srv := New(Config{})
	srv.listenAndServe = func(_ string, _ http.Handler) error { return nil }

	if err := srv.run(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestServerRun_ErrServerClosed(t *testing.T) {
	srv := New(Config{})
	srv.listenAndServe = func(_ string, _ http.Handler) error {
		return http.ErrServerClosed
	}

	// ErrServerClosed must be treated as a clean shutdown — not an error.
	if err := srv.run(); err != nil {
		t.Errorf("expected nil for ErrServerClosed, got %v", err)
	}
}

func TestServerRun_OtherError(t *testing.T) {
	want := errors.New("unexpected listen error")
	srv := New(Config{})
	srv.listenAndServe = func(_ string, _ http.Handler) error { return want }

	err := srv.run()
	if !errors.Is(err, want) {
		t.Errorf("expected error %v, got %v", want, err)
	}
}

// Start (package-level) is exercised in gofi's config package, where the
// SERVICE_DEBUG env wiring lives (config.StartDebug).

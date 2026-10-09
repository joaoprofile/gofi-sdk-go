package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//  DefaultCORSConfig

func TestDefaultCORSConfig(t *testing.T) {
	cfg := DefaultCORSConfig()
	assert.Contains(t, cfg.AllowedMethods, "GET")
	assert.Contains(t, cfg.AllowedMethods, "POST")
	assert.Contains(t, cfg.AllowedHeaders, "Content-Type")
	assert.Contains(t, cfg.AllowedHeaders, "Authorization")
	assert.True(t, cfg.AllowCredentials)
	assert.Empty(t, cfg.AllowedOrigins)
	assert.NoError(t, cfg.Validate())
}

//  Validate

func TestCorsConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     CorsConfig
		wantErr string
	}{
		{"exact origins", CorsConfig{AllowedOrigins: []string{"https://a.com", "http://localhost:3000", "capacitor://localhost"}, AllowCredentials: true}, ""},
		{"any origin without credentials", CorsConfig{AllowedOrigins: []string{"*"}}, ""},
		{"any origin with credentials", CorsConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}, "cannot be combined"},
		{"subdomain wildcard", CorsConfig{AllowedOrigins: []string{"https://*.a.com"}}, "invalid CORS origin"},
		{"path", CorsConfig{AllowedOrigins: []string{"https://a.com/app"}}, "invalid CORS origin"},
		{"no scheme", CorsConfig{AllowedOrigins: []string{"a.com"}}, "invalid CORS origin"},
		{"query", CorsConfig{AllowedOrigins: []string{"https://a.com?x=1"}}, "invalid CORS origin"},
		{"userinfo", CorsConfig{AllowedOrigins: []string{"https://u:p@a.com"}}, "invalid CORS origin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestCORSMiddleware_PanicsOnInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		CORSMiddleware(CorsConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
	})
}

//  CORSMiddleware — actual requests

func corsGet(t *testing.T, cfg CorsConfig, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)
	return rec
}

func TestCORSMiddleware_AllowedOrigin(t *testing.T) {
	cfg := CorsConfig{AllowedOrigins: []string{"https://gofi.com"}, AllowCredentials: true, ExposeHeaders: []string{"X-Custom"}}
	rec := corsGet(t, cfg, "https://gofi.com")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://gofi.com", rec.Header().Get(headerACAO))
	assert.Equal(t, "true", rec.Header().Get(headerACAC))
	assert.Equal(t, "X-Custom", rec.Header().Get(headerACEH))
	assert.Equal(t, []string{"Origin"}, rec.Header().Values("Vary"))
	assert.Empty(t, rec.Header().Get(headerACAM), "preflight headers only on preflights")
}

func TestCORSMiddleware_DisallowedOrMissingOrigin(t *testing.T) {
	cfg := CorsConfig{AllowedOrigins: []string{"https://allowed.com"}, AllowCredentials: true}
	for _, origin := range []string{"https://evil.com", "", "null", "https://allowed.com.evil.com"} {
		rec := corsGet(t, cfg, origin)
		assert.Empty(t, rec.Header().Get(headerACAO), origin)
		assert.Empty(t, rec.Header().Get(headerACAC), origin)
	}
}

// Regression: "*" was stored literally and never matched.
func TestCORSMiddleware_AnyOrigin(t *testing.T) {
	rec := corsGet(t, CorsConfig{AllowedOrigins: []string{"*"}}, "https://whoever.com")
	assert.Equal(t, "*", rec.Header().Get(headerACAO))
	assert.Empty(t, rec.Header().Get(headerACAC))
}

// Regression: normalizeOrigin removed ":443" anywhere (http://x:443, :4430).
func TestCORSMiddleware_DefaultPortsOnly(t *testing.T) {
	cfg := CorsConfig{AllowedOrigins: []string{"https://gofi.com", "http://api.gofi.com"}}
	assert.Equal(t, "https://gofi.com:443", corsGet(t, cfg, "https://gofi.com:443").Header().Get(headerACAO))
	assert.Equal(t, "http://api.gofi.com:80", corsGet(t, cfg, "http://api.gofi.com:80").Header().Get(headerACAO))
	assert.Empty(t, corsGet(t, cfg, "https://gofi.com:4430").Header().Get(headerACAO))
	assert.Empty(t, corsGet(t, cfg, "http://gofi.com:443").Header().Get(headerACAO))
	assert.Empty(t, corsGet(t, cfg, "http://gofi.com").Header().Get(headerACAO), "scheme matters")
}

//  CORSMiddleware — preflight

func preflight(t *testing.T, h http.Handler, path, origin, method, headers string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, path, nil)
	req.Header.Set("Origin", origin)
	req.Header.Set(headerACRM, method)
	if headers != "" {
		req.Header.Set(headerACRH, headers)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins:   []string{"https://gofi.com"},
		AllowedMethods:   []string{"GET", "PUT"},
		AllowCredentials: true,
		MaxAge:           "600",
	}
	called := false
	h := CORSMiddleware(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := preflight(t, h, "/", "https://gofi.com", "PUT", "content-type, X-Evil, authorization")
	assert.False(t, called, "next must not run on a preflight")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://gofi.com", rec.Header().Get(headerACAO))
	assert.Equal(t, "true", rec.Header().Get(headerACAC))
	assert.Equal(t, "GET, PUT", rec.Header().Get(headerACAM))
	assert.Equal(t, "600", rec.Header().Get(headerACMA))
	// Regression: requested headers were reflected verbatim.
	assert.Equal(t, "Content-Type, Authorization", rec.Header().Get(headerACAH))
	assert.ElementsMatch(t, []string{"Origin", headerACRM, headerACRH}, rec.Header().Values("Vary"))
}

// Regression: Access-Control-Request-Method was not validated.
func TestCORSMiddleware_Preflight_RejectsMethodAndOrigin(t *testing.T) {
	h := CORSMiddleware(CorsConfig{AllowedOrigins: []string{"https://gofi.com"}, AllowedMethods: []string{"GET"}})(okHandler())

	rec := preflight(t, h, "/", "https://gofi.com", "DELETE", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get(headerACAO))

	rec = preflight(t, h, "/", "https://evil.com", "GET", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Header().Get(headerACAO))
}

func TestCORSMiddleware_Preflight_CustomAllowedHeaders(t *testing.T) {
	h := CORSMiddleware(CorsConfig{AllowedOrigins: []string{"*"}, AllowedHeaders: []string{"X-Tenant"}})(okHandler())
	rec := preflight(t, h, "/", "https://a.com", "POST", "x-tenant, authorization, x-tenant")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get(headerACAO))
	assert.Equal(t, "X-Tenant", rec.Header().Get(headerACAH))
}

func TestCORSMiddleware_PlainOptionsReachesNext(t *testing.T) {
	called := false
	h := CORSMiddleware(DefaultCORSConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodOptions, "/", nil))
	assert.True(t, called, "OPTIONS without Origin/Access-Control-Request-Method is not a preflight")
}

//  normalizeOrigin

func TestNormalizeOrigin(t *testing.T) {
	cases := map[string]string{
		"https://gofi.com/":       "https://gofi.com",
		"  https://gofi.com  ":    "https://gofi.com",
		"HTTPS://GOFI.com:443":    "https://gofi.com",
		"http://gofi.com:80":      "http://gofi.com",
		"http://gofi.com:443":     "http://gofi.com:443",
		"https://gofi.com:4430":   "https://gofi.com:4430",
		"https://gofi.com:8080":   "https://gofi.com:8080",
		"https://[::1]:443":       "https://[::1]",
		"capacitor://localhost":   "capacitor://localhost",
		"https://gofi.com:443/":   "https://gofi.com",
		"https://gofi.com:44300/": "https://gofi.com:44300",
	}
	for in, want := range cases {
		got, ok := normalizeOrigin(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "null", "gofi.com", "https://gofi.com/x", "https://*.gofi.com", "https://gofi.com#f"} {
		_, ok := normalizeOrigin(bad)
		assert.False(t, ok, bad)
	}
}

//  Server — per-route policy replaces the global one

type corsRoutes struct{}

func (corsRoutes) Handlers() []*Route {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	return PublicRoutes("/api",
		GET("/global").To(ok),
		POST("/global").To(ok),
		GET("/public").To(ok).Cors(&CorsConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}}),
		POST("/partner").To(ok).Cors(&CorsConfig{AllowedOrigins: []string{"https://partner.com"}, AllowedMethods: []string{"POST"}, AllowCredentials: true}),
		GET("/partner").To(ok),
		GET("/").To(ok).Cors(&CorsConfig{AllowedOrigins: []string{"https://root.com"}}),
	)
}

func corsServer(t *testing.T) http.Handler {
	t.Helper()
	srv := NewServer(&WSConfig{AllowedOrigins: []string{"https://app.com"}}).(*httpServer)
	srv.AddHandlers(corsRoutes{})
	return srv.handler()
}

func serverRequest(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Regression: the global policy leaked into routes with their own policy.
func TestServer_RoutePolicyReplacesGlobal(t *testing.T) {
	h := corsServer(t)

	rec := serverRequest(h, http.MethodPost, "/api/partner", map[string]string{"Origin": "https://app.com"})
	assert.Empty(t, rec.Header().Get(headerACAO), "global origin must not reach a route with its own policy")
	assert.Empty(t, rec.Header().Get(headerACAC))

	rec = serverRequest(h, http.MethodPost, "/api/partner", map[string]string{"Origin": "https://partner.com"})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://partner.com", rec.Header().Get(headerACAO))
	assert.Equal(t, "true", rec.Header().Get(headerACAC))

	rec = serverRequest(h, http.MethodGet, "/api/public", map[string]string{"Origin": "https://app.com"})
	assert.Equal(t, "*", rec.Header().Get(headerACAO))
	assert.Empty(t, rec.Header().Get(headerACAC), "credentials must not survive the route policy")

	rec = serverRequest(h, http.MethodGet, "/api/global", map[string]string{"Origin": "https://app.com"})
	assert.Equal(t, "https://app.com", rec.Header().Get(headerACAO))
}

// Regression: the global middleware answered every OPTIONS, so route
// preflights never applied.
func TestServer_RoutePreflight(t *testing.T) {
	h := corsServer(t)

	rec := preflight(t, h, "/api/partner", "https://partner.com", "POST", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://partner.com", rec.Header().Get(headerACAO))
	assert.NotEmpty(t, rec.Header().Get("X-Content-Type-Options"), "security headers on preflights")

	rec = preflight(t, h, "/api/partner", "https://app.com", "POST", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// GET on the same path keeps the global policy.
	rec = preflight(t, h, "/api/partner", "https://app.com", "GET", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://app.com", rec.Header().Get(headerACAO))

	rec = preflight(t, h, "/api/public", "https://any.com", "GET", "")
	assert.Equal(t, "*", rec.Header().Get(headerACAO))

	for _, path := range []string{"/api", "/api/"} {
		rec = preflight(t, h, path, "https://root.com", "GET", "")
		assert.Equal(t, "https://root.com", rec.Header().Get(headerACAO), path)
	}

	rec = preflight(t, h, "/api/global", "https://app.com", "POST", "")
	assert.Equal(t, "https://app.com", rec.Header().Get(headerACAO))
}

func TestServer_InvalidCORSPanics(t *testing.T) {
	assert.Panics(t, func() { NewServer(&WSConfig{AllowedOrigins: []string{"*"}}) }, "* with default credentials")
	assert.NotPanics(t, func() {
		NewServer(&WSConfig{CORS: &CorsConfig{AllowedOrigins: []string{"*"}}})
	})

	srv := NewServer(&WSConfig{})
	bad := routesFunc(func() []*Route {
		return PublicRoutes("/x", GET("/").To(func(http.ResponseWriter, *http.Request) {}).
			Cors(&CorsConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}))
	})
	assert.Panics(t, func() { srv.AddHandlers(bad) })
}

func TestWSConfigValidate(t *testing.T) {
	assert.NoError(t, (&WSConfig{TrustedProxies: []string{"private", "10.0.0.0/8"}}).Validate())
	err := (&WSConfig{TrustedProxies: []string{"nope"}, AllowedOrigins: []string{"https://a.com/x"}}).Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "trusted proxy")
	assert.ErrorContains(t, err, "invalid CORS origin")
}

type routesFunc func() []*Route

func (f routesFunc) Handlers() []*Route { return f() }

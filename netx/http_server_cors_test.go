package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

//  DefaultCORSConfig

func TestDefaultCORSConfig_HasExpectedMethods(t *testing.T) {
	cfg := DefaultCORSConfig()
	assert.Contains(t, cfg.AllowedMethods, "GET")
	assert.Contains(t, cfg.AllowedMethods, "POST")
	assert.Contains(t, cfg.AllowedMethods, "OPTIONS")
}

func TestDefaultCORSConfig_HasExpectedHeaders(t *testing.T) {
	cfg := DefaultCORSConfig()
	assert.Contains(t, cfg.AllowedHeaders, "Content-Type")
	assert.Contains(t, cfg.AllowedHeaders, "Authorization")
}

func TestDefaultCORSConfig_AllowCredentials(t *testing.T) {
	cfg := DefaultCORSConfig()
	assert.True(t, cfg.AllowCredentials)
}

func TestDefaultCORSConfig_EmptyAllowedOriginsByDefault(t *testing.T) {
	cfg := DefaultCORSConfig()
	assert.Empty(t, cfg.AllowedOrigins)
}

//  CORSMiddleware — allowed origin ─

func TestCORSMiddleware_AllowedOrigin_SetsOriginHeader(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins:   []string{"https://gofi.com"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: true,
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://gofi.com")
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, "https://gofi.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORSMiddleware_AllowedOrigin_CallsNext(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins: []string{"https://gofi.com"},
		AllowedMethods: []string{"GET"},
	}

	called := false
	handler := CORSMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://gofi.com")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.True(t, called)
}

func TestCORSMiddleware_DisallowedOrigin_DoesNotSetOriginHeader(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins: []string{"https://allowed.com"},
		AllowedMethods: []string{"GET"},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_NoOriginHeader_DoesNotSetOriginHeader(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins: []string{"https://gofi.com"},
		AllowedMethods: []string{"GET"},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddleware_SetsVaryHeaders(t *testing.T) {
	cfg := CorsConfig{AllowedOrigins: []string{"https://gofi.com"}, AllowedMethods: []string{"GET"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	assert.Contains(t, vary, "Origin")
	assert.Contains(t, vary, "Access-Control-Request-Method")
	assert.Contains(t, vary, "Access-Control-Request-Headers")
}

func TestCORSMiddleware_SetsAllowedMethodsHeader(t *testing.T) {
	cfg := CorsConfig{AllowedMethods: []string{"GET", "POST"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestCORSMiddleware_SetsExposeHeadersWhenProvided(t *testing.T) {
	cfg := CorsConfig{AllowedMethods: []string{"GET"}, ExposeHeaders: []string{"X-Custom-Header"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, "X-Custom-Header", rec.Header().Get("Access-Control-Expose-Headers"))
}

func TestCORSMiddleware_SetsMaxAgeWhenProvided(t *testing.T) {
	cfg := CorsConfig{AllowedMethods: []string{"GET"}, MaxAge: "3600"}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, "3600", rec.Header().Get("Access-Control-Max-Age"))
}

func TestCORSMiddleware_EchoesRequestHeaders(t *testing.T) {
	cfg := CorsConfig{AllowedOrigins: []string{"https://gofi.com"}, AllowedMethods: []string{"GET"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Access-Control-Request-Headers", "X-Custom, Authorization")
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, "X-Custom, Authorization", rec.Header().Get("Access-Control-Allow-Headers"))
}

//  CORSMiddleware — preflight (OPTIONS)

func TestCORSMiddleware_Preflight_Returns204(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins: []string{"https://gofi.com"},
		AllowedMethods: []string{"GET", "POST"},
	}

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://gofi.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	called := false
	CORSMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.False(t, called, "next must NOT be called on preflight")
}

func TestCORSMiddleware_Preflight_AllowedOrigin_SetsHeaders(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins:   []string{"https://gofi.com"},
		AllowedMethods:   []string{"PUT"},
		AllowCredentials: true,
	}

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://gofi.com")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Equal(t, "https://gofi.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Content-Type", rec.Header().Get("Access-Control-Allow-Headers"))
}

func TestCORSMiddleware_Preflight_DisallowedOrigin_NoOriginHeader(t *testing.T) {
	cfg := CorsConfig{
		AllowedOrigins: []string{"https://allowed.com"},
		AllowedMethods: []string{"GET"},
	}

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	CORSMiddleware(cfg)(okHandler()).ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

//  normalizeOrigin

func TestNormalizeOrigin_TrimsTrailingSlash(t *testing.T) {
	assert.Equal(t, "https://gofi.com", normalizeOrigin("https://gofi.com/"))
}

func TestNormalizeOrigin_TrimsLeadingAndTrailingSpaces(t *testing.T) {
	assert.Equal(t, "https://gofi.com", normalizeOrigin("  https://gofi.com  "))
}

func TestNormalizeOrigin_RemovesPort443(t *testing.T) {
	assert.Equal(t, "https://gofi.com", normalizeOrigin("https://gofi.com:443"))
}

func TestNormalizeOrigin_PreservesOtherPorts(t *testing.T) {
	assert.Equal(t, "https://gofi.com:8080", normalizeOrigin("https://gofi.com:8080"))
}

func TestNormalizeOrigin_EmptyString_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", normalizeOrigin(""))
}

//  buildOriginMap ─

func TestBuildOriginMap_ContainsAllOrigins(t *testing.T) {
	m := buildOriginMap([]string{"https://a.com", "https://b.com"})
	_, hasA := m["https://a.com"]
	_, hasB := m["https://b.com"]
	assert.True(t, hasA)
	assert.True(t, hasB)
}

func TestBuildOriginMap_EmptyInput_ReturnsEmptyMap(t *testing.T) {
	m := buildOriginMap([]string{})
	assert.Empty(t, m)
}

//  isAllowedOrigin

func TestIsAllowedOrigin_KnownOrigin_ReturnsTrue(t *testing.T) {
	m := map[string]struct{}{"https://gofi.com": {}}
	assert.True(t, isAllowedOrigin("https://gofi.com", m))
}

func TestIsAllowedOrigin_UnknownOrigin_ReturnsFalse(t *testing.T) {
	m := map[string]struct{}{"https://gofi.com": {}}
	assert.False(t, isAllowedOrigin("https://other.com", m))
}

func TestIsAllowedOrigin_EmptyOrigin_ReturnsFalse(t *testing.T) {
	m := map[string]struct{}{"https://gofi.com": {}}
	assert.False(t, isAllowedOrigin("", m))
}

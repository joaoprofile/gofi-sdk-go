package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GET / POST / PUT / DELETE / PATCH ---

func TestGET_CreatesBuilderWithGetMethod(t *testing.T) {
	rb := GET("/users")
	require.NotNil(t, rb)
	assert.Equal(t, http.MethodGet, rb.method)
	assert.Equal(t, "/users", rb.rootPath)
}

func TestPOST_CreatesBuilderWithPostMethod(t *testing.T) {
	rb := POST("/users")
	require.NotNil(t, rb)
	assert.Equal(t, http.MethodPost, rb.method)
	assert.Equal(t, "/users", rb.rootPath)
}

func TestPUT_CreatesBuilderWithPutMethod(t *testing.T) {
	rb := PUT("/users/1")
	require.NotNil(t, rb)
	assert.Equal(t, http.MethodPut, rb.method)
	assert.Equal(t, "/users/1", rb.rootPath)
}

func TestDELETE_CreatesBuilderWithDeleteMethod(t *testing.T) {
	rb := DELETE("/users/1")
	require.NotNil(t, rb)
	assert.Equal(t, http.MethodDelete, rb.method)
	assert.Equal(t, "/users/1", rb.rootPath)
}

func TestPATCH_CreatesBuilderWithPatchMethod(t *testing.T) {
	rb := PATCH("/users/1")
	require.NotNil(t, rb)
	assert.Equal(t, http.MethodPatch, rb.method)
	assert.Equal(t, "/users/1", rb.rootPath)
}

// --- RouteBuilder.To ---

func TestTo_SetsHandler(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {}
	rb := GET("/path").To(handler)

	require.NotNil(t, rb.handler)
}

func TestTo_ReturnsBuilderForChaining(t *testing.T) {
	rb := GET("/path")
	result := rb.To(func(w http.ResponseWriter, r *http.Request) {})

	assert.Same(t, rb, result)
}

// --- RouteBuilder.Cors ---

func TestCors_SetsCorsConfig(t *testing.T) {
	cfg := &CorsConfig{AllowedOrigins: []string{"https://example.com"}}
	rb := GET("/path").Cors(cfg)

	assert.Equal(t, cfg, rb.corsConfig)
}

func TestCors_ReturnsBuilderForChaining(t *testing.T) {
	rb := GET("/path")
	result := rb.Cors(&CorsConfig{})

	assert.Same(t, rb, result)
}

func TestCors_NilConfigIsAccepted(t *testing.T) {
	rb := GET("/path").Cors(nil)
	assert.Nil(t, rb.corsConfig)
}

// --- RouteBuilder.Build ---

func TestBuild_PrefixWithoutTrailingSlashAndRootWithoutLeadingSlash(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/api", rootPath: "users"}
	route := rb.Build()

	assert.Equal(t, "/api/users", route.path)
}

func TestBuild_PrefixWithTrailingSlashAndRootWithoutLeadingSlash(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/api/", rootPath: "users"}
	route := rb.Build()

	assert.Equal(t, "/api/users", route.path)
}

func TestBuild_PrefixWithoutTrailingSlashAndRootWithLeadingSlash(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/api", rootPath: "/users"}
	route := rb.Build()

	assert.Equal(t, "/api/users", route.path)
}

func TestBuild_BothPrefixAndRootWithSlashes_NoDoubleSlash(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/api/", rootPath: "/users"}
	route := rb.Build()

	assert.Equal(t, "/api/users", route.path)
	assert.NotContains(t, route.path, "//")
}

func TestBuild_EmptyPrefixAndRootWithoutLeadingSlash(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "", rootPath: "users"}
	route := rb.Build()

	assert.Equal(t, "/users", route.path)
}

func TestBuild_SetsMethodFromBuilder(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodPost, prefix: "/api", rootPath: "/items"}
	route := rb.Build()

	assert.Equal(t, http.MethodPost, route.method)
}

func TestBuild_SetsAuthenticationFromBuilder(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/", rootPath: "x", authentication: true}
	route := rb.Build()

	assert.True(t, route.authentication)
}

func TestBuild_SetsCorsConfigFromBuilder(t *testing.T) {
	cfg := &CorsConfig{AllowedOrigins: []string{"*"}}
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/", rootPath: "x", corsConfig: cfg}
	route := rb.Build()

	assert.Equal(t, cfg, route.corsConfig)
}

func TestBuild_SetsPrefixFromBuilder(t *testing.T) {
	rb := &RouteBuilder{method: http.MethodGet, prefix: "/api", rootPath: "/v1"}
	route := rb.Build()

	assert.Equal(t, "/api", route.prefix)
}

func TestBuild_SetsHandlerFromBuilder(t *testing.T) {
	called := false
	handler := func(w http.ResponseWriter, r *http.Request) { called = true }

	rb := &RouteBuilder{method: http.MethodGet, prefix: "/", rootPath: "/x", handler: handler}
	route := rb.Build()

	require.NotNil(t, route.handler)
	route.handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.True(t, called)
}

// --- PublicRoutes ---

func TestPublicRoutes_SetsAuthenticationFalse(t *testing.T) {
	routes := PublicRoutes("/api", GET("/users").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.False(t, routes[0].authentication)
}

func TestPublicRoutes_AddsLeadingSlashToPrefix(t *testing.T) {
	routes := PublicRoutes("api", GET("/x").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.True(t, len(routes[0].path) > 0)
	assert.Equal(t, "/api", routes[0].prefix)
}

func TestPublicRoutes_EmptyPrefixBecomesSlash(t *testing.T) {
	routes := PublicRoutes("", GET("/health").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.Equal(t, "/", routes[0].prefix)
}

func TestPublicRoutes_MultipleBuilders(t *testing.T) {
	routes := PublicRoutes("/api",
		GET("/a").To(func(w http.ResponseWriter, r *http.Request) {}),
		POST("/b").To(func(w http.ResponseWriter, r *http.Request) {}),
		DELETE("/c").To(func(w http.ResponseWriter, r *http.Request) {}),
	)

	assert.Len(t, routes, 3)
	for _, r := range routes {
		assert.False(t, r.authentication)
	}
}

func TestPublicRoutes_BuiltPathIncludesPrefix(t *testing.T) {
	routes := PublicRoutes("/v1", GET("/items").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.Equal(t, "/v1/items", routes[0].path)
}

// --- PrivateRoutes ---

func TestPrivateRoutes_SetsAuthenticationTrue(t *testing.T) {
	routes := PrivateRoutes("/api", GET("/secure").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.True(t, routes[0].authentication)
}

func TestPrivateRoutes_AddsLeadingSlashToPrefix(t *testing.T) {
	routes := PrivateRoutes("private", GET("/data").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.Equal(t, "/private", routes[0].prefix)
}

func TestPrivateRoutes_EmptyPrefixBecomesSlash(t *testing.T) {
	routes := PrivateRoutes("", GET("/data").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.Equal(t, "/", routes[0].prefix)
}

func TestPrivateRoutes_MultipleBuilders(t *testing.T) {
	routes := PrivateRoutes("/admin",
		GET("/users").To(func(w http.ResponseWriter, r *http.Request) {}),
		DELETE("/users/1").To(func(w http.ResponseWriter, r *http.Request) {}),
	)

	assert.Len(t, routes, 2)
	for _, r := range routes {
		assert.True(t, r.authentication)
	}
}

func TestPrivateRoutes_BuiltPathIncludesPrefix(t *testing.T) {
	routes := PrivateRoutes("/admin", GET("/reports").To(func(w http.ResponseWriter, r *http.Request) {}))

	require.Len(t, routes, 1)
	assert.Equal(t, "/admin/reports", routes[0].path)
}

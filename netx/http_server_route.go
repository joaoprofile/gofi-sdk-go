package netx

import (
	"net/http"
	"strings"
)

type Middleware func(http.Handler) http.Handler

type handlerFunc func(http.ResponseWriter, *http.Request)

type CorsConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           string
}

type Route struct {
	method         string
	path           string
	handler        handlerFunc
	authentication bool
	corsConfig     *CorsConfig
	prefix         string
}

type RouteBuilder struct {
	prefix         string
	method         string
	rootPath       string
	handler        handlerFunc
	authentication bool
	corsConfig     *CorsConfig
}

type RouterHandler interface {
	Handlers() []*Route
}

type WSConfig struct {
	ServerPort     string
	AllowedOrigins []string

	// StressControl overrides the default concurrency limits. When nil,
	// sensible defaults (50 concurrent, 20 ms timeout) are applied.
	StressControl *StressControlConfig

	// RateLimiter configures per-client rate limiting. When nil, rate limiting
	// is disabled. Build the Backend field with NewRedisBackend(client) at the
	// composition root where the Redis client is available.
	RateLimiter *RedisRateLimiterConfig
}

func (rb *RouteBuilder) Build() *Route {
	fullPath := rb.prefix
	if !strings.HasSuffix(fullPath, "/") && !strings.HasPrefix(rb.rootPath, "/") {
		fullPath += "/"
	}
	fullPath += rb.rootPath

	fullPath = strings.ReplaceAll(fullPath, "//", "/")

	return &Route{
		method:         rb.method,
		path:           fullPath,
		handler:        rb.handler,
		authentication: rb.authentication,
		corsConfig:     rb.corsConfig,
		prefix:         rb.prefix,
	}
}

func (rb *RouteBuilder) To(function handlerFunc) *RouteBuilder {
	rb.handler = function
	return rb
}

func (rb *RouteBuilder) Cors(config *CorsConfig) *RouteBuilder {
	rb.corsConfig = config
	return rb
}

func GET(rootPath string) *RouteBuilder {
	return &RouteBuilder{method: http.MethodGet, rootPath: rootPath}
}
func POST(rootPath string) *RouteBuilder {
	return &RouteBuilder{method: http.MethodPost, rootPath: rootPath}
}
func PUT(rootPath string) *RouteBuilder {
	return &RouteBuilder{method: http.MethodPut, rootPath: rootPath}
}
func DELETE(rootPath string) *RouteBuilder {
	return &RouteBuilder{method: http.MethodDelete, rootPath: rootPath}
}
func PATCH(rootPath string) *RouteBuilder {
	return &RouteBuilder{method: http.MethodPatch, rootPath: rootPath}
}

func addRoute(isPrivateRoute bool, prefix string, builders ...*RouteBuilder) []*Route {
	var routes []*Route

	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if prefix == "" {
		prefix = "/"
	}

	for _, builder := range builders {
		builder.authentication = isPrivateRoute
		builder.prefix = prefix
		routes = append(routes, builder.Build())
	}
	return routes
}

func PrivateRoutes(prefix string, builders ...*RouteBuilder) []*Route {
	return addRoute(true, prefix, builders...)
}

func PublicRoutes(prefix string, builders ...*RouteBuilder) []*Route {
	return addRoute(false, prefix, builders...)
}

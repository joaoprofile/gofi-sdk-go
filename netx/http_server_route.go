package netx

import (
	"net/http"
	"strings"
	"time"
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
	readTimeout    time.Duration
	writeTimeout   time.Duration
}

type RouteBuilder struct {
	prefix         string
	method         string
	rootPath       string
	handler        handlerFunc
	authentication bool
	corsConfig     *CorsConfig
	readTimeout    time.Duration
	writeTimeout   time.Duration
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

	// MaxBodyBytes caps request bodies, in bytes. A value <= 0 falls back to
	// DefaultMaxBodyBytes. Services accepting large uploads must raise this
	// cap together with ReadTimeout and WriteTimeout: the body cap is useless
	// while the transfer cannot fit in the read budget.
	MaxBodyBytes int64

	// ReadTimeout bounds reading the whole request, body included. A value
	// <= 0 falls back to DefaultReadTimeout. ReadHeaderTimeout stays fixed at
	// 5s regardless — it is what guards against Slowloris, so relaxing this
	// field for slow uploads does not weaken that defense.
	ReadTimeout time.Duration

	// WriteTimeout bounds the response write. The deadline is armed right
	// after the request headers are read, so it also covers the time spent
	// receiving the body. A value <= 0 falls back to DefaultWriteTimeout.
	WriteTimeout time.Duration

	// IdleTimeout bounds keep-alive connections between requests. A value
	// <= 0 falls back to DefaultIdleTimeout.
	IdleTimeout time.Duration

	// RequestTimeout bounds the per-request context passed to handlers. A
	// value <= 0 falls back to DefaultRequestTimeout.
	RequestTimeout time.Duration
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
		readTimeout:    rb.readTimeout,
		writeTimeout:   rb.writeTimeout,
	}
}

func (rb *RouteBuilder) To(function handlerFunc) *RouteBuilder {
	rb.handler = function
	return rb
}

// Timeouts overrides the server-wide read and write deadlines for this route
// alone, so a service can accept a long upload without relaxing ReadTimeout
// for every other route. Either argument may be zero to keep the server-wide
// value for that direction.
//
// Only the connection deadlines can be extended this way. WSConfig.RequestTimeout
// is a context deadline: downstream code can shorten it, never lengthen it, so a
// route that needs a long handler budget still requires a matching server-wide
// RequestTimeout.
func (rb *RouteBuilder) Timeouts(read, write time.Duration) *RouteBuilder {
	rb.readTimeout = read
	rb.writeTimeout = write
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

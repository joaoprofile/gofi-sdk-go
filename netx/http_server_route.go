package netx

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

type Middleware func(http.Handler) http.Handler

type handlerFunc func(http.ResponseWriter, *http.Request)

// CorsConfig is a CORS policy. A route policy (RouteBuilder.Cors) replaces
// the global one for that route, preflights included.
type CorsConfig struct {
	// AllowedOrigins are exact scheme://host[:port] origins, or AnyOrigin
	// ("*", only with AllowCredentials=false).
	AllowedOrigins []string
	// AllowedMethods are checked against Access-Control-Request-Method;
	// empty uses DefaultCORSConfig's.
	AllowedMethods []string
	// AllowedHeaders bound Access-Control-Allow-Headers; empty uses
	// DefaultCORSAllowedHeaders.
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
	ServerPort string

	// AllowedOrigins sets the origins of the global CORS policy, which is
	// otherwise DefaultCORSConfig. Ignored when CORS is set.
	AllowedOrigins []string

	// CORS replaces the global CORS policy, e.g. AnyOrigin without credentials.
	CORS *CorsConfig

	// TrustedProxies lists the CIDRs of load balancers allowed to set
	// X-Forwarded-For. nil or empty trusts none: the TCP peer is the client.
	// The entry TrustPrivateNetworks ("private") expands to PrivateNetworks().
	TrustedProxies []string

	// StressControl overrides the default concurrency limits. When nil, the
	// defaults apply: DefaultMaxConcurrent() slots shared by all clients,
	// DefaultStressTimeout wait, DefaultStressBufferBytes buffered per body.
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

	// ShutdownTimeout bounds graceful shutdown. A value <= 0 falls back to
	// DefaultShutdownTimeout; keep it below the pod's termination grace period.
	ShutdownTimeout time.Duration

	// DrainDelay is waited after readiness fails and before shutdown starts.
	// 0 uses DefaultDrainDelay when Health is enabled, otherwise no delay.
	DrainDelay time.Duration

	// Health enables /livez and /readyz; nil disables them.
	Health *HealthConfig

	// H2C serves HTTP/2 without TLS next to HTTP/1.1, for meshes and load
	// balancers that speak HTTP/2 to the pod. It cannot be combined with TLS.
	H2C bool

	// TLS serves HTTPS (HTTP/2 and HTTP/1.1), optionally with mTLS; nil serves
	// plain HTTP. Invalid settings make ListenAndServe fail.
	TLS *TLSConfig

	// DisableCrossOriginProtection turns off the CSRF check. By default every
	// route rejects cross-origin unsafe requests (http.CrossOriginProtection,
	// by Sec-Fetch-Site/Origin) except from the origins its CORS policy
	// allows; same-origin requests and clients sending neither header pass.
	DisableCrossOriginProtection bool

	// HSTS configures Strict-Transport-Security; it is only sent over TLS or
	// behind a trusted proxy reporting X-Forwarded-Proto: https.
	HSTS HSTSConfig

	// ExposeErrorCause returns wrapped error causes in ErrorResponse.Cause
	// (RespondError). Keep false in production: causes may carry SQL or
	// driver details.
	ExposeErrorCause bool
}

// Validate reports the settings NewServer would panic on.
func (c *WSConfig) Validate() error {
	var errs []error
	if err := c.corsConfig().Validate(); err != nil {
		errs = append(errs, err)
	}
	if _, err := parsePrefixes(c.TrustedProxies); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// corsConfig is the global CORS policy.
func (c *WSConfig) corsConfig() CorsConfig {
	if c.CORS != nil {
		return *c.CORS
	}
	cfg := DefaultCORSConfig()
	if len(c.AllowedOrigins) > 0 {
		cfg.AllowedOrigins = c.AllowedOrigins
	}
	return cfg
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

// Cors gives the route its own CORS policy, replacing the global one;
// AddHandlers panics when it is invalid (see CorsConfig.Validate).
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

package netx

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// ── Security headers ─────────────────────────────────────────────────────────

// SecurityHeaders sets defensive response headers protecting against
// clickjacking, XSS, MIME sniffing, HSTS bypass, and server fingerprinting.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self'; "+
				"img-src 'self' data:; "+
				"object-src 'none'; "+
				"frame-ancestors 'none';")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		h.Set("Server", "")
		h.Set("X-Powered-By", "")

		next.ServeHTTP(w, r)
	})
}

// BlockUnsafeMethods rejects TRACE (information disclosure) and CONNECT
// (proxy tunnel abuse) before they reach application handlers.
func BlockUnsafeMethods(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodTrace, http.MethodConnect:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LimitBody caps request bodies at 10 MB to prevent memory exhaustion attacks.
func LimitBody(next http.Handler) http.Handler {
	const maxBody = 10 << 20 // 10 MB

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

// ValidateRequest rejects requests that carry both Content-Length and
// Transfer-Encoding, a classic HTTP request-smuggling vector.
func ValidateRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Length") != "" && r.TransferEncoding != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Stress / overload control ─────────────────────────────────────────────────

// RouteLimit defines per-route concurrency constraints.
type RouteLimit struct {
	Path          string
	MaxConcurrent int
	Timeout       time.Duration
}

// StressControlConfig holds global and per-route concurrency settings.
type StressControlConfig struct {
	DefaultMaxConcurrent int
	DefaultTimeout       time.Duration
	RouteLimits          []RouteLimit
}

// NewStressControlMiddleware limits the number of concurrent requests served
// simultaneously. Route-specific limits are checked first by prefix; all
// other requests fall through to the default limiter.
func NewStressControlMiddleware(cfg StressControlConfig) Middleware {
	type entry struct {
		path    string
		handler Middleware
	}

	defaultLimiter := newSemaphoreLimiter(cfg.DefaultMaxConcurrent, cfg.DefaultTimeout)

	routeLimiters := make([]entry, 0, len(cfg.RouteLimits))
	for _, rl := range cfg.RouteLimits {
		routeLimiters = append(routeLimiters, entry{
			path:    rl.Path,
			handler: newSemaphoreLimiter(rl.MaxConcurrent, rl.Timeout),
		})
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, rl := range routeLimiters {
				if strings.HasPrefix(r.URL.Path, rl.path) {
					rl.handler(next).ServeHTTP(w, r)
					return
				}
			}
			defaultLimiter(next).ServeHTTP(w, r)
		})
	}
}

// newSemaphoreLimiter returns a middleware that allows at most max concurrent
// requests. If the semaphore is not acquired within timeout, the request is
// rejected with 503. The context deadline or client cancellation also
// releases a waiting request immediately.
func newSemaphoreLimiter(max int, timeout time.Duration) Middleware {
	if max <= 0 {
		max = 100
	}
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}

	sem := make(chan struct{}, max)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			case <-ctx.Done():
				http.Error(w, "server busy", http.StatusServiceUnavailable)
			}
		})
	}
}

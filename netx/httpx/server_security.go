package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// ── Security headers ─────────────────────────────────────────────────────────

// securityHeaders are the fixed hardening headers. The value slices are
// shared across requests: Header.Set replaces a slice and Header.Add on a
// full slice reallocates, so handlers never mutate them.
var securityHeaders = []struct {
	key   string
	value []string
}{
	{"Referrer-Policy", []string{"strict-origin-when-cross-origin"}},
	{"Permissions-Policy", []string{"geolocation=(), microphone=(), camera=()"}},
	{"X-Content-Type-Options", []string{"nosniff"}},
	{"X-Frame-Options", []string{"DENY"}},
	{"Content-Security-Policy", []string{"default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self'; " +
		"img-src 'self' data:; " +
		"object-src 'none'; " +
		"frame-ancestors 'none';"}},
	// API responses carry per-user data; handlers set their own to cache.
	{"Cache-Control", []string{"no-store"}},
}

// DefaultHSTSMaxAge is two years, the preload list minimum.
const DefaultHSTSMaxAge = 2 * 365 * 24 * time.Hour

// HSTSConfig configures Strict-Transport-Security. The zero value sends
// max-age=63072000; includeSubDomains.
type HSTSConfig struct {
	// Disabled omits the header, e.g. when the ingress sets it.
	Disabled bool
	// MaxAge defaults to DefaultHSTSMaxAge when <= 0.
	MaxAge time.Duration
	// ExcludeSubDomains drops includeSubDomains.
	ExcludeSubDomains bool
	// Preload adds preload. Opt in only when every subdomain serves HTTPS:
	// leaving the preload list takes months.
	Preload bool
}

func (c HSTSConfig) value() string {
	v := "max-age=" + strconv.FormatInt(int64(orDefault(c.MaxAge, DefaultHSTSMaxAge)/time.Second), 10)
	if !c.ExcludeSubDomains {
		v += "; includeSubDomains"
	}
	if c.Preload {
		v += "; preload"
	}
	return v
}

// SecurityHeadersConfig configures SecurityHeadersWith.
type SecurityHeadersConfig struct {
	HSTS HSTSConfig
	// TrustedProxies (CIDRs or TrustPrivateNetworks) may report HTTPS with
	// X-Forwarded-Proto; invalid entries panic.
	TrustedProxies []string
}

// SecurityHeaders sets defensive response headers against clickjacking, XSS,
// MIME sniffing and caching of API responses, plus HSTS over TLS.
func SecurityHeaders(next http.Handler) http.Handler {
	return SecurityHeadersWith(SecurityHeadersConfig{})(next)
}

// SecurityHeadersWith is SecurityHeaders with HSTS settings; HSTS is sent over
// TLS, or when a trusted proxy reports X-Forwarded-Proto: https.
func SecurityHeadersWith(cfg SecurityHeadersConfig) Middleware {
	trusted := mustPrefixes(cfg.TrustedProxies)
	var hsts []string
	if !cfg.HSTS.Disabled {
		hsts = []string{cfg.HSTS.value()}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			for _, sh := range securityHeaders {
				h[sh.key] = sh.value // keys are already canonical
			}
			if hsts != nil && isHTTPS(r, trusted) {
				h["Strict-Transport-Security"] = hsts
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isHTTPS reports a TLS connection, or a trusted proxy that terminated TLS.
func isHTTPS(r *http.Request, trusted []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	if len(trusted) == 0 {
		return false
	}
	proto, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
	return strings.EqualFold(strings.TrimSpace(proto), "https") && inPrefixes(peerIP(r), trusted)
}

// Recoverer turns a handler panic into a generic 500 and logs the panic with
// the request ID, trace and stack. http.ErrAbortHandler is re-raised, so
// net/http aborts the response as intended.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			ctx := r.Context()
			logging.FromContext(ctx).LogAttrs(ctx, slog.LevelError, "panic recovered",
				slog.String("request_id", netx.GetRequestID(ctx)),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("panic", fmt.Sprint(rec)),
				slog.String("stack", string(debug.Stack())),
			)
			internalError(w)
		}()
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

// DefaultMaxBodyBytes is the request body cap applied when no explicit limit
// is configured.
const DefaultMaxBodyBytes int64 = 10 << 20 // 10 MB

// LimitBody caps request bodies at DefaultMaxBodyBytes to prevent memory
// exhaustion attacks. Kept for callers that do not need a custom cap.
func LimitBody(next http.Handler) http.Handler {
	return LimitBodyWithMax(DefaultMaxBodyBytes)(next)
}

// LimitBodyWithMax caps request bodies at maxBody bytes. A maxBody <= 0 falls
// back to DefaultMaxBodyBytes, so a missing configuration never leaves the
// server without a ceiling.
//
// Requests announcing an oversized Content-Length are rejected with 413 before
// the handler runs, so the caller gets an honest error instead of a parse
// failure surfacing deep inside the handler. Chunked requests carry no length
// upfront: those stay capped by MaxBytesReader, which fails on read.
func LimitBodyWithMax(maxBody int64) Middleware {
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBody {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			next.ServeHTTP(w, r)
		})
	}
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

// Stress control defaults, applied when StressControlConfig leaves a field <= 0.
const (
	// DefaultStressTimeout is how long a request waits for a slot before 503.
	DefaultStressTimeout = 100 * time.Millisecond
	// DefaultStressBufferBytes is how much of a request body is read before a
	// slot is taken.
	DefaultStressBufferBytes int64 = 64 << 10
)

// DefaultMaxConcurrent returns the default slot count: 64 per GOMAXPROCS, at
// least 256. Handlers are mostly I/O-bound (DB, downstream HTTP), so the cap
// guards memory and backend fan-out rather than CPU; a few dozen slots would
// turn ordinary latency spikes into 503s.
func DefaultMaxConcurrent() int {
	return max(256, 64*runtime.GOMAXPROCS(0))
}

// RouteLimit defines per-route concurrency constraints.
type RouteLimit struct {
	Path          string
	MaxConcurrent int
	Timeout       time.Duration
}

// StressControlConfig holds global and per-route concurrency settings. The
// limits are process-wide, shared by every client, not per client: pair them
// with the rate limiter for per-client fairness.
type StressControlConfig struct {
	// DefaultMaxConcurrent caps requests in flight outside RouteLimits;
	// <= 0 uses DefaultMaxConcurrent().
	DefaultMaxConcurrent int
	// DefaultTimeout is the wait for a slot; <= 0 uses DefaultStressTimeout.
	DefaultTimeout time.Duration
	// RouteLimits give path prefixes their own pool, e.g. uploads, so slow
	// routes cannot drain the default pool.
	RouteLimits []RouteLimit
	// BufferBodyBytes of each request body are read before a slot is taken,
	// so clients trickling a small body never hold a slot. Bodies above it
	// take the slot once the prefix arrived and are then bounded only by the
	// read timeouts. <= 0 uses DefaultStressBufferBytes.
	BufferBodyBytes int64
}

// NewStressControlMiddleware limits the number of concurrent requests served
// simultaneously. Route-specific limits are checked first by prefix; all
// other requests fall through to the default limiter.
func NewStressControlMiddleware(cfg StressControlConfig) Middleware {
	type entry struct {
		path    string
		limiter Middleware
	}

	bufferBytes := cfg.BufferBodyBytes
	if bufferBytes <= 0 {
		bufferBytes = DefaultStressBufferBytes
	}

	defaultLimiter := newSemaphoreLimiter(cfg.DefaultMaxConcurrent, cfg.DefaultTimeout)

	routeLimiters := make([]entry, 0, len(cfg.RouteLimits))
	for _, rl := range cfg.RouteLimits {
		routeLimiters = append(routeLimiters, entry{
			path:    rl.Path,
			limiter: newSemaphoreLimiter(rl.MaxConcurrent, rl.Timeout),
		})
	}

	return func(next http.Handler) http.Handler {
		defaultHandler := defaultLimiter(next)
		routeHandlers := make([]http.Handler, len(routeLimiters))
		for i, rl := range routeLimiters {
			routeHandlers[i] = rl.limiter(next)
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bufferBody(r, bufferBytes)
			for i, rl := range routeLimiters {
				if strings.HasPrefix(r.URL.Path, rl.path) {
					routeHandlers[i].ServeHTTP(w, r)
					return
				}
			}
			defaultHandler.ServeHTTP(w, r)
		})
	}
}

// bufferBody reads up to limit bytes of r.Body into memory and puts them back
// in front of the unread rest. The buffer grows with the bytes received, so a
// slow client costs memory in proportion to what it actually sent.
func bufferBody(r *http.Request, limit int64) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return
	}
	var buf bytes.Buffer
	_, err := buf.ReadFrom(io.LimitReader(r.Body, limit))
	rest := io.Reader(r.Body)
	if err != nil {
		rest = errReader{err} // keep the failure for the handler to see
	}
	r.Body = bufferedBody{Reader: io.MultiReader(&buf, rest), Closer: r.Body}
}

type bufferedBody struct {
	io.Reader
	io.Closer
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// newSemaphoreLimiter returns a middleware that allows at most max concurrent
// requests. If the semaphore is not acquired within timeout, the request is
// rejected with 503. The context deadline or client cancellation also
// releases a waiting request immediately.
func newSemaphoreLimiter(max int, timeout time.Duration) Middleware {
	if max <= 0 {
		max = DefaultMaxConcurrent()
	}
	if timeout <= 0 {
		timeout = DefaultStressTimeout
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
				w.Header().Set("Retry-After", "1")
				http.Error(w, "server busy", http.StatusServiceUnavailable)
			}
		})
	}
}

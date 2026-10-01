package netx

import (
	"cmp"
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

// RequestIDHeader carries the request ID in both directions.
const RequestIDHeader = "X-Request-Id"

// maxRequestIDLen bounds a client-supplied request ID.
const maxRequestIDLen = 64

type exposeCauseKey struct{}

// requestContext stores the request ID (the client's X-Request-Id when it is
// 1-64 of [A-Za-z0-9._-], a random one otherwise), echoes it in the response,
// and records whether RespondError may expose error causes.
func requestContext(exposeCause bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !validRequestID(id) {
				id = generateRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), RequestIDKey, id)
			ctx = context.WithValue(ctx, chiMiddleware.RequestIDKey, id)
			if exposeCause {
				ctx = context.WithValue(ctx, exposeCauseKey{}, true)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validRequestID accepts 1-64 characters of [A-Za-z0-9._-], so a client ID
// can neither forge log fields nor bloat them.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// exposeCause reports whether the server allows error causes in responses.
func exposeCause(r *http.Request) bool {
	if r == nil {
		return false
	}
	v, _ := r.Context().Value(exposeCauseKey{}).(bool)
	return v
}

// responseWriter wraps http.ResponseWriter to capture the status code written
// by the handler so the logging middleware can evaluate it after the fact.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush keeps streaming responses (SSE, chunked) working behind the logger.
func (rw *responseWriter) Flush() {
	_ = http.NewResponseController(rw.ResponseWriter).Flush()
}

// Unwrap exposes the wrapped ResponseWriter so http.ResponseController can
// walk down to the one holding the connection deadline setters. Without it,
// per-route deadlines silently degrade to the server-wide timeouts.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// LoggingMiddleware logs only errors and security-relevant responses.
// 2xx and 3xx responses are intentionally silent — logs are persisted and
// request-level noise would dominate the cost and signal-to-noise ratio.
//
// Logged conditions:
//   - 5xx  → Error  (server fault, always logged)
//   - 401/403/429 → Warn  (access denied, rate limited — security signal)
func LoggingMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			ctx := r.Context()
			reqID := GetRequestID(ctx)
			if reqID == "" { // used outside NewServer
				reqID = cmp.Or(chiMiddleware.GetReqID(ctx), generateRequestID())
				ctx = context.WithValue(ctx, RequestIDKey, reqID)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(rw, r)

			// Attributes are built only for the responses that are logged.
			status := rw.statusCode
			switch {
			case status >= 500:
				logging.FromContext(ctx).LogAttrs(ctx, slog.LevelError, "server error", requestAttrs(reqID, r, status, time.Since(start))...)
			case status == http.StatusUnauthorized,
				status == http.StatusForbidden,
				status == http.StatusTooManyRequests:
				logging.FromContext(ctx).LogAttrs(ctx, slog.LevelWarn, "access denied", requestAttrs(reqID, r, status, time.Since(start))...)
			}
		})
	}
}

// LogRateLimit logs a rate-limit event at Warn level with trace context.
func LogRateLimit(r *http.Request) {
	logging.FromContext(r.Context()).Warn("rate limit exceeded",
		slog.String("ip", clientIP(r)),
		apiKeyAttr(r),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogInvalidAPIKey logs an invalid API key attempt at Warn level.
func LogInvalidAPIKey(r *http.Request) {
	logging.FromContext(r.Context()).Warn("invalid api key",
		slog.String("ip", clientIP(r)),
		apiKeyAttr(r),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogIPBlocked logs a blocked IP attempt at Warn level.
func LogIPBlocked(r *http.Request, clientName string) {
	logging.FromContext(r.Context()).Warn("ip not allowed",
		slog.String("ip", clientIP(r)),
		slog.String("client", clientName),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogServerError logs an application error at Error level with trace context.
func LogServerError(r *http.Request, err error) {
	logging.FromContext(r.Context()).Error("server error",
		slog.String("ip", clientIP(r)),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Any("error", err),
	)
}

// GetRequestID returns the request ID stored in the context by LoggingMiddleware.
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(RequestIDKey).(string)
	return id
}

func requestAttrs(reqID string, r *http.Request, status int, latency time.Duration) []slog.Attr {
	return []slog.Attr{
		slog.String("request_id", reqID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("ip", clientIP(r)),
		slog.Duration("latency", latency),
	}
}

// generateRequestID returns 128 random bits (crypto/rand, base32).
func generateRequestID() string {
	return rand.Text()
}

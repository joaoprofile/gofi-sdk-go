package netx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/joaoprofile/gofi/obs/logging"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

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

			reqID := chiMiddleware.GetReqID(r.Context())
			if reqID == "" {
				reqID = generateRequestID()
			}

			ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
			next.ServeHTTP(rw, r.WithContext(ctx))

			status := rw.statusCode
			log := logging.FromContext(ctx)
			attrs := requestAttrs(reqID, r, status, time.Since(start))

			switch {
			case status >= 500:
				log.Error("server error", attrs...)
			case status == http.StatusUnauthorized,
				status == http.StatusForbidden,
				status == http.StatusTooManyRequests:
				log.Warn("access denied", attrs...)
			}
		})
	}
}

// LogRateLimit logs a rate-limit event at Warn level with trace context.
func LogRateLimit(r *http.Request) {
	logging.FromContext(r.Context()).Warn("rate limit exceeded",
		slog.String("ip", extractIP(r)),
		slog.String("api_key", extractAPIKey(r)),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogInvalidAPIKey logs an invalid API key attempt at Warn level.
func LogInvalidAPIKey(r *http.Request) {
	logging.FromContext(r.Context()).Warn("invalid api key",
		slog.String("ip", extractIP(r)),
		slog.String("api_key", extractAPIKey(r)),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogIPBlocked logs a blocked IP attempt at Warn level.
func LogIPBlocked(r *http.Request, clientName string) {
	logging.FromContext(r.Context()).Warn("ip not allowed",
		slog.String("ip", extractIP(r)),
		slog.String("client", clientName),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// LogServerError logs an application error at Error level with trace context.
func LogServerError(r *http.Request, err error) {
	logging.FromContext(r.Context()).Error("server error",
		slog.String("ip", extractIP(r)),
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

func requestAttrs(reqID string, r *http.Request, status int, latency time.Duration) []any {
	return []any{
		slog.String("request_id", reqID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("ip", extractIP(r)),
		slog.Duration("latency", latency),
	}
}

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

package netx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
)

// ── responseWriter.WriteHeader ────────────────────────────────────────────────

func TestResponseWriter_WriteHeader_CapturesStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusNotFound)

	assert.Equal(t, http.StatusNotFound, rw.statusCode)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestResponseWriter_WriteHeader_DefaultsTo200BeforeCall(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	assert.Equal(t, http.StatusOK, rw.statusCode)
}

// ── LoggingMiddleware ─────────────────────────────────────────────────────────

func TestLoggingMiddleware_Status200_CallsNextAndPassesThrough(t *testing.T) {
	called := false
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggingMiddleware_Status500_DoesNotPanic(t *testing.T) {
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() { mw.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoggingMiddleware_Status502_DoesNotPanic(t *testing.T) {
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() { mw.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestLoggingMiddleware_Status401_DoesNotPanic(t *testing.T) {
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() { mw.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoggingMiddleware_Status403_DoesNotPanic(t *testing.T) {
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() { mw.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLoggingMiddleware_Status429_DoesNotPanic(t *testing.T) {
	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() { mw.ServeHTTP(rec, req) })
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestLoggingMiddleware_SetsRequestIDInContext(t *testing.T) {
	var capturedID string

	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mw.ServeHTTP(httptest.NewRecorder(), req)

	assert.NotEmpty(t, capturedID)
}

func TestLoggingMiddleware_ReusesChiRequestID(t *testing.T) {
	// When chi's RequestID middleware has already populated the context, the
	// logging middleware must reuse that ID rather than generate a new one.
	var capturedID string

	handler := chiMiddleware.RequestID(
		LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		})),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "propagated-id")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "propagated-id", capturedID)
}

func TestLoggingMiddleware_DefaultStatusIs200WhenHandlerDoesNotWriteHeader(t *testing.T) {
	var capturedStatus int

	mw := LoggingMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler writes body without explicitly calling WriteHeader.
		// responseWriter should default to 200.
		_, _ = w.Write([]byte("ok"))
	}))

	// Verify that the request does not panic and the recorder captured 200.
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	capturedStatus = rec.Code
	assert.Equal(t, http.StatusOK, capturedStatus)
}

// ── GetRequestID ──────────────────────────────────────────────────────────────

func TestGetRequestID_ReturnsIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, "req-abc-123")
	assert.Equal(t, "req-abc-123", GetRequestID(ctx))
}

func TestGetRequestID_ReturnsEmptyStringWhenAbsent(t *testing.T) {
	assert.Empty(t, GetRequestID(context.Background()))
}

func TestGetRequestID_ReturnsEmptyForNilValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, nil)
	assert.Empty(t, GetRequestID(ctx))
}

// ── generateRequestID ─────────────────────────────────────────────────────────

func TestGenerateRequestID_ReturnsNonEmptyString(t *testing.T) {
	id := generateRequestID()
	assert.NotEmpty(t, id)
}

func TestGenerateRequestID_ReturnsHex16Chars(t *testing.T) {
	id := generateRequestID()
	// hex-encoded 8 bytes = 16 characters
	assert.Len(t, id, 16)
}

func TestGenerateRequestID_ProducesUniqueValues(t *testing.T) {
	const n = 20
	ids := make(map[string]struct{}, n)
	for range n {
		ids[generateRequestID()] = struct{}{}
	}
	assert.Len(t, ids, n, "all generated IDs must be unique")
}

// ── requestAttrs ──────────────────────────────────────────────────────────────

func TestRequestAttrs_ReturnsSixAttributes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/items", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	attrs := requestAttrs("test-id", req, http.StatusCreated, 12*time.Millisecond)

	// request_id, method, path, status, ip, latency
	assert.Len(t, attrs, 6)
}

// ── Log helpers ───────────────────────────────────────────────────────────────

func TestLogInvalidAPIKey_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/products", nil)
	req.Header.Set("X-API-Key", "bad-key-value")
	req.RemoteAddr = "10.0.0.5:8080"

	assert.NotPanics(t, func() {
		LogInvalidAPIKey(req)
	})
}

func TestLogIPBlocked_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/orders", nil)
	req.RemoteAddr = "192.168.1.50:443"

	assert.NotPanics(t, func() {
		LogIPBlocked(req, "client-name-xyz")
	})
}

func TestLogServerError_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.RemoteAddr = "172.16.0.1:9000"

	assert.NotPanics(t, func() {
		LogServerError(req, errors.New("database connection failed"))
	})
}

func TestLogServerError_WithNilError_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	assert.NotPanics(t, func() {
		LogServerError(req, nil)
	})
}

func TestLogRateLimit_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	req.Header.Set("X-API-Key", "rate-limited-key")
	req.RemoteAddr = "203.0.113.1:4321"

	assert.NotPanics(t, func() {
		LogRateLimit(req)
	})
}

func TestLogIPBlocked_WithXForwardedFor_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")

	assert.NotPanics(t, func() {
		LogIPBlocked(req, "blocked-client")
	})
}

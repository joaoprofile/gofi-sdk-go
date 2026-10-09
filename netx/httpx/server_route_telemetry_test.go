package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// recordingSpan keeps what routeTelemetry writes to the server span.
type recordingSpan struct {
	noop.Span
	name  string
	attrs []attribute.KeyValue
}

func (s *recordingSpan) SetName(name string)                    { s.name = name }
func (s *recordingSpan) SetAttributes(kv ...attribute.KeyValue) { s.attrs = append(s.attrs, kv...) }

type routeTelemetryHandler struct{}

func (routeTelemetryHandler) Handlers() []*Route {
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	return append(
		PublicRoutes("/orders", GET("/{id}").To(ok), POST("/").To(ok)),
		PrivateRoutes("/", GET("/me").To(ok), GET("/").To(ok))...,
	)
}

func TestRouteTelemetry_NamesSpanAndLabelsMetrics(t *testing.T) {
	ws := NewServer(&WSConfig{}).(*httpServer)
	ws.UseAuth(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	})
	ws.AddHandlers(routeTelemetryHandler{})

	cases := []struct {
		method, path, want string
		status             int
	}{
		{http.MethodGet, "/orders/42", "/orders/{id}", http.StatusOK},
		{http.MethodPost, "/orders/", "/orders", http.StatusOK},
		// Rejected by auth: still labeled with its route.
		{http.MethodGet, "/", "/", http.StatusUnauthorized},
		{http.MethodGet, "/me", "/me", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			span := &recordingSpan{}
			labeler := &otelhttp.Labeler{}
			ctx := otelhttp.ContextWithLabeler(trace.ContextWithSpan(context.Background(), span), labeler)

			rec := httptest.NewRecorder()
			ws.handler().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil).WithContext(ctx))

			require.Equal(t, tc.status, rec.Code)
			route := attribute.String("http.route", tc.want)
			assert.Equal(t, tc.method+" "+tc.want, span.name)
			assert.Contains(t, span.attrs, route)
			assert.Contains(t, labeler.Get(), route)
		})
	}
}

func TestRouteTelemetry_UnmatchedKeepsDefaultName(t *testing.T) {
	ws := NewServer(&WSConfig{}).(*httpServer)
	ws.AddHandlers(routeTelemetryHandler{})

	span := &recordingSpan{}
	ctx := trace.ContextWithSpan(context.Background(), span)
	rec := httptest.NewRecorder()
	ws.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil).WithContext(ctx))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, span.name, "404s have no route and must not create one series per path")
}

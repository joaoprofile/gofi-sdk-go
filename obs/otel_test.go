package obs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkl "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
)

func TestInit_DefaultVersion(t *testing.T) {
	ctx := context.Background()
	cfg := TeleConfig{
		ServiceName:    "test-service",
		ServiceVersion: "",
		ServiceEnv:     "test",
		CollectorAddr:  "localhost:4317",
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tele)

	assert.NotNil(t, tele.TracerProvider)
	assert.NotNil(t, tele.MeterProvider)
	assert.NotNil(t, tele.LoggerProvider)

	require.NoError(t, tele.Shutdown(ctx))
}

func TestInit_WithVersion(t *testing.T) {
	ctx := context.Background()
	cfg := TeleConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.2.3",
		ServiceEnv:     "staging",
		CollectorAddr:  "localhost:4317",
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tele)

	assert.NotNil(t, tele.TracerProvider)
	assert.NotNil(t, tele.MeterProvider)
	assert.NotNil(t, tele.LoggerProvider)

	require.NoError(t, tele.Shutdown(ctx))
}

func TestShutdown_NilProviders(t *testing.T) {
	tele := &Telemetry{}
	err := tele.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestShutdown_Idempotent(t *testing.T) {
	ctx := context.Background()
	cfg := TeleConfig{
		ServiceName:   "test-service",
		ServiceEnv:    "test",
		CollectorAddr: "localhost:4317",
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)

	assert.NoError(t, tele.Shutdown(ctx))
	// Second shutdown should return errors from already-shutdown providers,
	// but must not panic.
	_ = tele.Shutdown(ctx)
}

// --- Init error-path tests (use injectable factory vars) ---

// restoreFactories resets all package-level factory vars to their originals
// after the test completes.
func withFactoryStub[T any](t *testing.T, ptr *T, stub T) {
	t.Helper()
	orig := *ptr
	*ptr = stub
	t.Cleanup(func() { *ptr = orig })
}

func TestInit_GRPCClientError(t *testing.T) {
	// "%%bad%%" triggers a URL-parse error inside grpc.NewClient.
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: "%%bad%%"}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gRPC client")
}

func TestInit_TraceExporterError(t *testing.T) {
	withFactoryStub(t, &newTraceExp, func(_ context.Context, _ *grpc.ClientConn) (sdktrace.SpanExporter, error) {
		return nil, errors.New("trace exporter injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: "localhost:4317"}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace exporter")
}

func TestInit_MetricExporterError(t *testing.T) {
	withFactoryStub(t, &newMetricExp, func(_ context.Context, _ *grpc.ClientConn) (metric.Exporter, error) {
		return nil, errors.New("metric exporter injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: "localhost:4317"}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metric exporter")
}

func TestInit_RuntimeStartError(t *testing.T) {
	withFactoryStub(t, &startRuntime, func(_ *metric.MeterProvider) error {
		return errors.New("runtime start injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: "localhost:4317"}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime metrics")
}

func TestInit_LogExporterError(t *testing.T) {
	withFactoryStub(t, &newLogExp, func(_ context.Context, _ *grpc.ClientConn) (sdkl.Exporter, error) {
		return nil, errors.New("log exporter injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: "localhost:4317"}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log exporter")
}

// TestShutdown_WithExpiredContext covers the error-accumulation branches inside
// Shutdown by passing a context whose deadline is already in the past.
// OTel batch processors check ctx.Err() at flush time and propagate it,
// which causes the `errs = append(errs, ...)` lines to execute.
func TestShutdown_WithExpiredContext(t *testing.T) {
	ctx := context.Background()
	cfg := TeleConfig{
		ServiceName:   "test-service",
		ServiceEnv:    "test",
		CollectorAddr: "localhost:4317",
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)

	// Generate a span so the trace batch processor has pending work to flush.
	tracer := tele.TracerProvider.Tracer("test")
	_, span := tracer.Start(ctx, "test-op")
	span.End()

	// Context already past its deadline → providers return context.DeadlineExceeded.
	expiredCtx, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()

	// Must not panic; we don't assert on the specific error because SDK behaviour
	// may vary, but the error-accumulation code paths are exercised.
	_ = tele.Shutdown(expiredCtx)
}

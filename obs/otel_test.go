package obs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdklog "go.opentelemetry.io/otel/sdk/log"
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
		CollectorAddr:  startCollector(t),
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tele)

	assert.NotNil(t, tele.TracerProvider)
	assert.NotNil(t, tele.MeterProvider)

	require.NoError(t, tele.Shutdown(ctx))
}

func TestInit_WithVersion(t *testing.T) {
	ctx := context.Background()
	cfg := TeleConfig{
		ServiceName:    "test-service",
		ServiceVersion: "1.2.3",
		ServiceEnv:     "staging",
		CollectorAddr:  startCollector(t),
	}

	tele, err := Init(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, tele)

	assert.NotNil(t, tele.TracerProvider)
	assert.NotNil(t, tele.MeterProvider)

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
		CollectorAddr: startCollector(t),
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
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: startCollector(t)}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trace exporter")
}

func TestInit_MetricExporterError(t *testing.T) {
	withFactoryStub(t, &newMetricExp, func(_ context.Context, _ *grpc.ClientConn) (metric.Exporter, error) {
		return nil, errors.New("metric exporter injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: startCollector(t)}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metric exporter")
}

func TestInit_RuntimeStartError(t *testing.T) {
	withFactoryStub(t, &startRuntime, func(_ *metric.MeterProvider) error {
		return errors.New("runtime start injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: startCollector(t)}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runtime metrics")
}

func TestInit_LogExporterError(t *testing.T) {
	withFactoryStub(t, &newLogExp, func(_ context.Context, _ *grpc.ClientConn) (sdklog.Exporter, error) {
		return nil, errors.New("log exporter injection error")
	})
	cfg := TeleConfig{ServiceName: "svc", ServiceEnv: "test", CollectorAddr: startCollector(t)}
	_, err := Init(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log exporter")
}

// Init attaches the OTLP log handler to an initialized global logger, and the
// logger provider is flushed by Telemetry.Shutdown.
func TestInit_AttachesLogsToGlobalLogger(t *testing.T) {
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)
	require.NoError(t, logging.InitGlobal(context.Background(), logging.Config{ServiceName: "svc"}))
	before := logging.Instance()

	tele, err := Init(context.Background(), TeleConfig{ServiceName: "svc", CollectorAddr: startCollector(t)})
	require.NoError(t, err)
	assert.NotNil(t, tele.LoggerProvider)
	assert.NotSame(t, before, logging.Instance(), "the global logger must be rebuilt with the OTLP handler")

	logging.Info("exported")
	assert.NoError(t, tele.Shutdown(context.Background()))
}

// Without an initialized global logger, Init still succeeds; logs are not exported.
func TestInit_WithoutGlobalLogger(t *testing.T) {
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)

	tele, err := Init(context.Background(), TeleConfig{ServiceName: "svc", CollectorAddr: startCollector(t)})
	require.NoError(t, err)
	assert.NoError(t, tele.Shutdown(context.Background()))
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
		CollectorAddr: startCollector(t),
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

func TestInit_RegistersW3CPropagator(t *testing.T) {
	tele, err := Init(context.Background(), TeleConfig{ServiceName: "svc", CollectorAddr: startCollector(t)})
	require.NoError(t, err)
	defer tele.Shutdown(context.Background())

	fields := otel.GetTextMapPropagator().Fields()
	assert.Contains(t, fields, "traceparent")
	assert.Contains(t, fields, "baggage")
}

func TestNewResource_Attributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "k8s.namespace.name=orders")
	res, err := newResource(context.Background(), TeleConfig{ServiceName: "svc", ServiceEnv: "prod"})
	require.NoError(t, err)

	got := map[string]string{}
	for _, kv := range res.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	assert.Equal(t, "svc", got["service.name"])
	assert.Equal(t, "prod", got["deployment.environment.name"])
	assert.Equal(t, "prod", got["environment"], "legacy attribute kept")
	assert.Equal(t, "orders", got["k8s.namespace.name"], "OTEL_RESOURCE_ATTRIBUTES honoured")
	assert.Equal(t, "unknown", got["service.version"])
}

func TestNewResource_Version(t *testing.T) {
	version := func(cfg TeleConfig) string {
		t.Helper()
		res, err := newResource(context.Background(), cfg)
		require.NoError(t, err)
		v, _ := res.Set().Value("service.version")
		return v.String()
	}

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.version=2.0.0")
	assert.Equal(t, "2.0.0", version(TeleConfig{ServiceName: "svc"}), "taken from OTEL_RESOURCE_ATTRIBUTES")
	assert.Equal(t, "1.2.3", version(TeleConfig{ServiceName: "svc", ServiceVersion: "1.2.3"}), "config wins over the environment")

	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	assert.Equal(t, "unknown", version(TeleConfig{ServiceName: "svc"}))
}

func TestInit_FailureReleasesProviders(t *testing.T) {
	withFactoryStub(t, &startRuntime, func(*metric.MeterProvider) error { return errors.New("boom") })
	tele, err := Init(context.Background(), TeleConfig{ServiceName: "svc", CollectorAddr: startCollector(t)})
	assert.Nil(t, tele)
	assert.ErrorContains(t, err, "runtime metrics")
}

func TestInit_LegacyMetricNames(t *testing.T) {
	tele, err := Init(context.Background(), TeleConfig{ServiceName: "svc", CollectorAddr: startCollector(t), LegacyMetricNames: true})
	require.NoError(t, err)
	assert.NoError(t, tele.Shutdown(context.Background()))
}

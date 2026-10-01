package observability

import (
	"context"
	"net"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/obs"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	colllogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

type fakeTraceService struct {
	colltrace.UnimplementedTraceServiceServer
}

func (fakeTraceService) Export(context.Context, *colltrace.ExportTraceServiceRequest) (*colltrace.ExportTraceServiceResponse, error) {
	return &colltrace.ExportTraceServiceResponse{}, nil
}

type fakeMetricService struct {
	collmetric.UnimplementedMetricsServiceServer
}

func (fakeMetricService) Export(context.Context, *collmetric.ExportMetricsServiceRequest) (*collmetric.ExportMetricsServiceResponse, error) {
	return &collmetric.ExportMetricsServiceResponse{}, nil
}

type fakeLogsService struct {
	colllogs.UnimplementedLogsServiceServer
}

func (fakeLogsService) Export(context.Context, *colllogs.ExportLogsServiceRequest) (*colllogs.ExportLogsServiceResponse, error) {
	return &colllogs.ExportLogsServiceResponse{}, nil
}

// startCollector runs an in-process OTLP/gRPC collector that accepts every
// export, so Init/Shutdown flush without a real collector on localhost:4317.
func startCollector(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	colltrace.RegisterTraceServiceServer(srv, fakeTraceService{})
	collmetric.RegisterMetricsServiceServer(srv, fakeMetricService{})
	colllogs.RegisterLogsServiceServer(srv, fakeLogsService{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestStartWithoutEndpointIsSkipped(t *testing.T) {
	logging.NewLogger("test")
	rt := gofi.NewRuntime(&environment.Environment{})
	c := New()
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Nil(t, c.Telemetry())
}

func TestStartWithEndpointAndShutdown(t *testing.T) {
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)
	logging.NewLogger("test")
	rt := gofi.NewRuntime(&environment.Environment{AppName: "svc", OtelExporterOTLPEndpoint: "http://" + startCollector(t)})

	c := New()
	require.NoError(t, c.Start(context.Background(), rt))
	require.NotNil(t, c.Telemetry())
	assert.NotNil(t, c.Telemetry().LoggerProvider, "logs are exported with traces and metrics")
	assert.NoError(t, rt.Close(context.Background()))
}

func TestFromTelemetryIsNotShutDown(t *testing.T) {
	tele := &obs.Telemetry{}
	rt := gofi.NewRuntime(&environment.Environment{OtelExporterOTLPEndpoint: "unused:4317"})
	c := FromTelemetry(tele)
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Same(t, tele, c.Telemetry())
	assert.NoError(t, rt.Close(context.Background()))
}

func TestStartInitErrorFails(t *testing.T) {
	logging.NewLogger("test")
	// "%%bad%%" makes grpc.NewClient fail on the target URL.
	rt := gofi.NewRuntime(&environment.Environment{OtelExporterOTLPEndpoint: "%%bad%%"})
	c := New()
	assert.Error(t, c.Start(context.Background(), rt))
	assert.Nil(t, c.Telemetry())
}

func TestStartRejectsTLSFilesWithPlaintextEndpoint(t *testing.T) {
	logging.NewLogger("test")
	rt := gofi.NewRuntime(&environment.Environment{
		OtelExporterOTLPEndpoint:    "http://" + startCollector(t),
		OtelExporterOTLPCertificate: "/tls/ca.pem",
	})
	c := New()
	err := c.Start(context.Background(), rt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OTEL_EXPORTER_OTLP_CERTIFICATE")
	assert.Nil(t, c.Telemetry())
}

func TestStartUnreadableCAFails(t *testing.T) {
	logging.NewLogger("test")
	rt := gofi.NewRuntime(&environment.Environment{
		OtelExporterOTLPEndpoint:    "https://otel.invalid:4317",
		OtelExporterOTLPCertificate: "/does/not/exist.pem",
	})
	c := New()
	assert.ErrorIs(t, c.Start(context.Background(), rt), obs.ErrInvalidTLS, "no fallback to plaintext")
	assert.Nil(t, c.Telemetry())
}

func TestInsecureTransports(t *testing.T) {
	logging.NewLogger("test")
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "prod", OtelExporterOTLPEndpoint: "http://otel:4317"}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, "otlp", found[0].Resource)
	assert.Equal(t, "collector otel:4317 without TLS", found[0].Detail)
	assert.ErrorIs(t, core.CheckTransport(env, found), core.ErrInsecureTransport, "refused in prod")
	env.AllowInsecureTransport = "otlp"
	assert.NoError(t, core.CheckTransport(env, found), "allowed by the hatch")

	for _, ok := range []*environment.Environment{
		{OtelExporterOTLPEndpoint: "otel:4317"}, // TLS by default
		{OtelExporterOTLPEndpoint: "http://localhost:4317"},
		{OtelExporterOTLPEndpoint: "unix:///var/run/otel.sock", OtelExporterOTLPInsecure: "true"},
		{},
	} {
		assert.Empty(t, New().InsecureTransports(ok), ok.OtelExporterOTLPEndpoint)
	}
	assert.Empty(t, FromTelemetry(nil).InsecureTransports(env), "injected telemetry")
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "observability", New().Name())
	assert.Equal(t, gofi.StageObservability, New().Stage())
}

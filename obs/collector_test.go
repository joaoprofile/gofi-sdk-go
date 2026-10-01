package obs

import (
	"context"
	"net"
	"testing"

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
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	colltrace.RegisterTraceServiceServer(srv, fakeTraceService{})
	collmetric.RegisterMetricsServiceServer(srv, fakeMetricService{})
	colllogs.RegisterLogsServiceServer(srv, fakeLogsService{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

package obs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	logsexporter "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	sdkl "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TeleConfig struct {
	ServiceName    string
	ServiceVersion string // defaults to "unknown" when empty
	ServiceEnv     string
	CollectorAddr  string
}

// Telemetry holds the three OTel providers and the underlying gRPC connection.
// Call Shutdown to flush pending data and release all resources.
type Telemetry struct {
	conn           *grpc.ClientConn
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
	LoggerProvider *sdkl.LoggerProvider
}

// Package-level factory vars allow tests to inject stubs for each I/O-bound
// operation inside Init, exercising error-handling paths without real infra.
var (
	newGRPCClient = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return grpc.NewClient(target, opts...)
	}
	newTraceExp = func(ctx context.Context, conn *grpc.ClientConn) (sdktrace.SpanExporter, error) {
		return otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	}
	newMetricExp = func(ctx context.Context, conn *grpc.ClientConn) (metric.Exporter, error) {
		return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	}
	newLogExp = func(ctx context.Context, conn *grpc.ClientConn) (sdkl.Exporter, error) {
		return logsexporter.New(ctx, logsexporter.WithGRPCConn(conn))
	}
	startRuntime = func(mp *metric.MeterProvider) error {
		return runtime.Start(runtime.WithMeterProvider(mp))
	}
)

// Init creates and registers the TracerProvider, MeterProvider and LoggerProvider,
// all sharing a single gRPC connection to the OTLP collector.
func Init(ctx context.Context, cfg TeleConfig) (*Telemetry, error) {
	version := cfg.ServiceVersion
	if version == "" {
		version = "unknown"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(version),
			attribute.String("environment", cfg.ServiceEnv),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	conn, err := newGRPCClient(
		cfg.CollectorAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	// Tracer Provider
	traceExporter, err := newTraceExp(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Meter Provider
	metricExporter, err := newMetricExp(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(5*time.Second))),
		metric.WithView(
			// --- PROCESS ---
			metric.NewView(metric.Instrument{Name: "process.cpu.utilization"}, metric.Stream{Name: "gofi_process_cpu_utilization"}),
			metric.NewView(metric.Instrument{Name: "process.memory.usage"}, metric.Stream{Name: "gofi_process_memory_usage"}),
			metric.NewView(metric.Instrument{Name: "process.open_file_descriptors"}, metric.Stream{Name: "gofi_process_fds_open"}),

			// --- GOROUTINES & SCHEDULING ---
			metric.NewView(metric.Instrument{Name: "go.goroutine.count"}, metric.Stream{Name: "gofi_go_goroutines"}),
			metric.NewView(metric.Instrument{Name: "go.processor.limit"}, metric.Stream{Name: "gofi_go_processor_limit"}),
			metric.NewView(metric.Instrument{Name: "go.schedule.quanta"}, metric.Stream{Name: "gofi_go_schedule_quanta_total"}),

			// --- MEMORY ---
			metric.NewView(metric.Instrument{Name: "go.memory.allocated"}, metric.Stream{Name: "gofi_go_mem_heap_alloc"}),
			metric.NewView(metric.Instrument{Name: "go.memory.used"}, metric.Stream{Name: "gofi_go_mem_used"}),
			metric.NewView(metric.Instrument{Name: "go.memory.allocations"}, metric.Stream{Name: "gofi_go_mem_allocations_total"}),
			metric.NewView(metric.Instrument{Name: "go.memory.frees"}, metric.Stream{Name: "gofi_go_mem_frees_total"}),
			metric.NewView(metric.Instrument{Name: "go.memory.gc.goal"}, metric.Stream{Name: "gofi_go_mem_gc_goal"}),

			// --- GC ---
			metric.NewView(metric.Instrument{Name: "go.cpu.gc.time"}, metric.Stream{Name: "gofi_go_cpu_gc_usage"}),
			metric.NewView(metric.Instrument{Name: "go.config.gogc"}, metric.Stream{Name: "gofi_go_gc_config_gogc"}),

			// --- HTTP ---
			metric.NewView(
				metric.Instrument{Name: "http.server.request.duration"},
				metric.Stream{Name: "gofi_http_server_request_duration", Description: "Duration of HTTP server requests."},
			),
		),
	)
	otel.SetMeterProvider(mp)

	if err := startRuntime(mp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to start runtime metrics: %w", err)
	}

	// Logger Provider
	logExporter, err := newLogExp(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}
	lp := sdkl.NewLoggerProvider(
		sdkl.WithProcessor(sdkl.NewBatchProcessor(logExporter)),
		sdkl.WithResource(res),
	)
	global.SetLoggerProvider(lp)

	return &Telemetry{
		conn:           conn,
		TracerProvider: tp,
		MeterProvider:  mp,
		LoggerProvider: lp,
	}, nil
}

// Shutdown flushes and closes all providers and the underlying gRPC connection.
// All errors are accumulated and returned together via errors.Join.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error

	if t.LoggerProvider != nil {
		if err := t.LoggerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("logger provider: %w", err))
		}
	}
	if t.MeterProvider != nil {
		if err := t.MeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider: %w", err))
		}
	}
	if t.TracerProvider != nil {
		if err := t.TracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider: %w", err))
		}
	}
	if t.conn != nil {
		if err := t.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("grpc connection: %w", err))
		}
	}

	return errors.Join(errs...)
}

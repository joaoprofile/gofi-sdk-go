package obs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc"
)

type TeleConfig struct {
	ServiceName string
	// ServiceVersion falls back to service.version in OTEL_RESOURCE_ATTRIBUTES,
	// then to "unknown".
	ServiceVersion string
	ServiceEnv     string // exported as deployment.environment.name
	CollectorAddr  string

	// TLS encrypts the OTLP gRPC connection; the default is plaintext for
	// in-cluster collectors. The TLS fields below need TLS=true: setting any
	// of them without it makes Init fail instead of sending plaintext.
	TLS bool

	// TLSConfig is used as is when set; the file fields and ServerName are
	// then ignored.
	TLSConfig *tls.Config
	// CAFile is a PEM bundle trusted for the collector certificate. As the
	// OTel spec defines for OTEL_EXPORTER_OTLP_CERTIFICATE, it replaces the
	// system roots; empty verifies against the system roots.
	CAFile string
	// CertFile and KeyFile are the PEM client certificate and key (mTLS);
	// set both or neither.
	CertFile string
	KeyFile  string
	// ServerName overrides the name verified in the collector certificate
	// (and sent as SNI); empty uses the dialed host.
	ServerName string

	// MetricInterval overrides OTEL_METRIC_EXPORT_INTERVAL (SDK default 60s).
	MetricInterval time.Duration

	// MetricCardinalityLimit caps the attribute sets kept per instrument in
	// each collection; extra sets fold into one otel.metric.overflow series,
	// so unbounded labels cannot exhaust memory. 0 keeps the SDK default
	// (2000, or OTEL_GO_X_CARDINALITY_LIMIT); negative removes the cap.
	MetricCardinalityLimit int

	// LegacyMetricNames keeps the pre-v0.3 gofi_* metric names for existing
	// dashboards instead of the OpenTelemetry semantic-convention names.
	LegacyMetricNames bool
}

// Telemetry holds the tracer, meter and logger providers and the gRPC
// connection they share. Call Shutdown to flush.
type Telemetry struct {
	conn           *grpc.ClientConn
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
}

func meterProviderOptions(cfg TeleConfig, res *resource.Resource, reader metric.Reader) []metric.Option {
	opts := []metric.Option{metric.WithResource(res), metric.WithReader(reader)}
	switch {
	case cfg.MetricCardinalityLimit > 0:
		opts = append(opts, metric.WithCardinalityLimit(cfg.MetricCardinalityLimit))
	case cfg.MetricCardinalityLimit < 0:
		opts = append(opts, metric.WithCardinalityLimit(0))
	}
	if cfg.LegacyMetricNames {
		opts = append(opts, metric.WithView(legacyViews()...))
	}
	return opts
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
	newLogExp = func(ctx context.Context, conn *grpc.ClientConn) (sdklog.Exporter, error) {
		return otlploggrpc.New(ctx, otlploggrpc.WithGRPCConn(conn))
	}
	startRuntime = func(mp *metric.MeterProvider) error {
		return runtime.Start(runtime.WithMeterProvider(mp))
	}
)

// Init creates and registers the TracerProvider, MeterProvider, LoggerProvider
// and the W3C trace-context/baggage propagator over one gRPC connection to the
// collector. The resource merges the SDK defaults, OTEL_RESOURCE_ATTRIBUTES and
// host, container and SDK attributes.
//
// Logs are exported by attaching an OTLP handler to the global logger
// (logging.Attach); when logging.InitGlobal has not run, logs are not exported
// and a warning is printed.
func Init(ctx context.Context, cfg TeleConfig) (tele *Telemetry, err error) {
	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	creds, err := transportCredentials(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := newGRPCClient(cfg.CollectorAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	t := &Telemetry{conn: conn}
	defer func() {
		// Release whatever was created when a later step fails.
		if err != nil {
			err = errors.Join(err, t.Shutdown(context.WithoutCancel(ctx)))
			tele = nil
		}
	}()

	traceExporter, err := newTraceExp(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	t.TracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	metricExporter, err := newMetricExp(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}
	var readerOpts []metric.PeriodicReaderOption
	if cfg.MetricInterval > 0 {
		readerOpts = append(readerOpts, metric.WithInterval(cfg.MetricInterval))
	}
	t.MeterProvider = metric.NewMeterProvider(
		meterProviderOptions(cfg, res, metric.NewPeriodicReader(metricExporter, readerOpts...))...)

	if err := startRuntime(t.MeterProvider); err != nil {
		return nil, fmt.Errorf("failed to start runtime metrics: %w", err)
	}

	logExporter, err := newLogExp(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}
	t.LoggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	// The provider is flushed by Telemetry.Shutdown, not by logging.Shutdown.
	err = logging.Attach(otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(t.LoggerProvider)), nil)
	if errors.Is(err, logging.ErrNotInitialized) {
		logging.Warn("obs: logs are not exported because the global logger is not initialized")
		err = nil
	}
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(t.TracerProvider)
	otel.SetMeterProvider(t.MeterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return t, nil
}

func newResource(ctx context.Context, cfg TeleConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{semconv.ServiceName(cfg.ServiceName)}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.ServiceEnv != "" {
		attrs = append(attrs,
			semconv.DeploymentEnvironmentName(cfg.ServiceEnv),
			attribute.String("environment", cfg.ServiceEnv), // pre-v0.3 attribute kept for existing queries
		)
	}
	detected, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithContainer(),
		resource.WithAttributes(attrs...),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), detected)
	if err != nil {
		return nil, err
	}
	if _, ok := res.Set().Value(semconv.ServiceVersionKey); ok {
		return res, nil
	}
	return resource.Merge(res, resource.NewSchemaless(semconv.ServiceVersion("unknown")))
}

// legacyViews renames standard runtime metrics to the pre-v0.3 gofi_* names.
func legacyViews() []metric.View {
	rename := func(from, to string) metric.View {
		return metric.NewView(metric.Instrument{Name: from}, metric.Stream{Name: to})
	}
	return []metric.View{
		rename("process.cpu.utilization", "gofi_process_cpu_utilization"),
		rename("process.memory.usage", "gofi_process_memory_usage"),
		rename("process.open_file_descriptors", "gofi_process_fds_open"),
		rename("go.goroutine.count", "gofi_go_goroutines"),
		rename("go.processor.limit", "gofi_go_processor_limit"),
		rename("go.schedule.quanta", "gofi_go_schedule_quanta_total"),
		rename("go.memory.allocated", "gofi_go_mem_heap_alloc"),
		rename("go.memory.used", "gofi_go_mem_used"),
		rename("go.memory.allocations", "gofi_go_mem_allocations_total"),
		rename("go.memory.frees", "gofi_go_mem_frees_total"),
		rename("go.memory.gc.goal", "gofi_go_mem_gc_goal"),
		rename("go.cpu.gc.time", "gofi_go_cpu_gc_usage"),
		rename("go.config.gogc", "gofi_go_gc_config_gogc"),
		rename("http.server.request.duration", "gofi_http_server_request_duration"),
	}
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

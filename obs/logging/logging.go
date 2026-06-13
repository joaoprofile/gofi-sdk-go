package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"

	"github.com/joaoprofile/gofi/base/common"
	"github.com/joaoprofile/gofi/base/environment"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	LOG_START_ERROR string = "\nCONFIGURATION ERROR: The global logger has not been initialized.\nMake sure to call InitGlobal() at the start of your service.\n"
)

var (
	instance *Logger
	once     sync.Once
)

func NewLogger(serviceName string) error {
	env := environment.Instance()
	return InitGlobal(context.Background(), Config{
		ServiceName:   serviceName,
		Environment:   env.GetEnvironmentType(),
		Level:         slogLevel(env.GetLogLevel()),
		CollectorAddr: env.OtelExporterOTLPEndpoint,
	})
}

func slogLevel(l common.LogLevel) slog.Level {
	switch l {
	case common.LogLevelDebug:
		return slog.LevelDebug
	case common.LogLevelWarn:
		return slog.LevelWarn
	case common.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func InitGlobal(ctx context.Context, cfg Config) error {
	var err error
	once.Do(func() {
		instance, err = New(ctx, cfg)
	})
	return err
}

// Instance returns the global logger. Panics if InitGlobal was never called.
func Instance() *Logger {
	if instance == nil {
		panic(LOG_START_ERROR)
	}
	return instance
}

// ResetForTesting resets the singleton so that InitGlobal re-initialises on the
// next call. Must only be called from tests.
func ResetForTesting() {
	once = sync.Once{}
	instance = nil
}

// --- Shortcuts ---

func Info(msg string, args ...any)  { Instance().Info(msg, args...) }
func Error(msg string, args ...any) { Instance().Error(msg, args...) }
func Debug(msg string, args ...any) { Instance().Debug(msg, args...) }
func Warn(msg string, args ...any)  { Instance().Warn(msg, args...) }

func Fatal(msg string, args ...any) {
	Instance().Error(msg, args...)
	os.Exit(1)
}

func FromContext(ctx context.Context) *slog.Logger {
	return Instance().FromContext(ctx)
}

func Shutdown(ctx context.Context) error {
	if instance != nil {
		return instance.Shutdown(ctx)
	}
	return nil
}

type Config struct {
	ServiceName   string
	Environment   environment.EnvironmentType
	EnableDebug   bool       // legado: equivale a Level=Debug
	Level         slog.Level // nível do handler (zero = Info); EnableDebug tem precedência
	CollectorAddr string     // quando vazio, usa apenas saída no console (sem OTLP)
}

type Logger struct {
	*slog.Logger
	env environment.EnvironmentType
	lp  *log.LoggerProvider
}

func New(ctx context.Context, cfg Config) (*Logger, error) {
	level := cfg.Level
	if cfg.EnableDebug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	var consoleHandler slog.Handler
	if cfg.Environment == environment.ENV_DEV {
		consoleHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		consoleHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// When no collector is configured, skip OTLP entirely.
	if cfg.CollectorAddr == "" {
		l := slog.New(consoleHandler)
		if cfg.ServiceName != "" {
			l = l.With("service", cfg.ServiceName)
		}
		slog.SetDefault(l)
		return &Logger{Logger: l, env: cfg.Environment}, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.DeploymentEnvironmentKey.String(string(cfg.Environment)),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	exporter, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(cfg.CollectorAddr),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	lp := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(exporter)),
	)

	otlpHandler := otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp))

	finalHandler := &TeeHandler{
		handlers: []slog.Handler{consoleHandler, otlpHandler},
	}

	l := slog.New(finalHandler)
	if cfg.ServiceName != "" {
		l = l.With("service", cfg.ServiceName)
	}
	slog.SetDefault(l)

	return &Logger{
		Logger: l,
		env:    cfg.Environment,
		lp:     lp,
	}, nil
}

// FromContext returns a logger enriched with trace_id and span_id from ctx.
func (l *Logger) FromContext(ctx context.Context) *slog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return l.Logger
	}
	return l.Logger.With(
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	)
}

// ErrorWithStack logs an error with the current goroutine stack trace.
func (l *Logger) ErrorWithStack(ctx context.Context, msg string, attrs ...any) {
	stack := string(debug.Stack())
	attrs = append(attrs, "stacktrace", stack)
	l.FromContext(ctx).Log(ctx, slog.LevelError, msg, attrs...)
}

func (l *Logger) Shutdown(ctx context.Context) error {
	if l.lp != nil {
		return l.lp.Shutdown(ctx)
	}
	return nil
}

// --- TeeHandler ---

// TeeHandler fans out log records to multiple slog.Handler implementations.
type TeeHandler struct {
	handlers []slog.Handler
}

func (t *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *TeeHandler) Handle(ctx context.Context, record slog.Record) error {
	var lastErr error
	for _, h := range t.handlers {
		if h.Enabled(ctx, record.Level) {
			if err := h.Handle(ctx, record); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

func (t *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &TeeHandler{handlers: newHandlers}
}

func (t *TeeHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &TeeHandler{handlers: newHandlers}
}

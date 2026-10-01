package logging

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gofi-labs/gofi-sdk-go/base/common"
	"go.opentelemetry.io/otel/trace"
)

const (
	LOG_START_ERROR string = "\nCONFIGURATION ERROR: The global logger has not been initialized.\nMake sure to call InitGlobal() at the start of your service.\n"
)

var (
	instance     atomic.Pointer[Logger]
	once         sync.Once
	fallbackOnce sync.Once
	attachMu     sync.Mutex
)

// ErrNotInitialized is returned by Attach when InitGlobal has not run yet.
var ErrNotInitialized = errors.New("logging: the global logger is not initialized; call InitGlobal first")

// NewLogger initialises the global logger with sane defaults (Info level, JSON
// console). It reads no environment; gofi's config.InitLogging builds
// an env-driven Config and is what services should use in production.
func NewLogger(serviceName string) error {
	return InitGlobal(context.Background(), Config{ServiceName: serviceName})
}

// SlogLevel maps a common.LogLevel to its slog.Level. Exported so gofi's config
// package can build a logging.Config from the environment.
func SlogLevel(l common.LogLevel) slog.Level {
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
		var l *Logger
		l, err = New(ctx, cfg)
		if l != nil {
			instance.Store(l)
		}
	})
	return err
}

// Instance returns the global logger. Before InitGlobal it falls back to
// slog.Default() (warning once), so SDK packages never crash an app that did
// not initialise gofi logging.
func Instance() *Logger {
	if l := instance.Load(); l != nil {
		return l
	}
	fallbackOnce.Do(func() { slog.Warn(strings.TrimSpace(LOG_START_ERROR)) })
	return &Logger{Logger: slog.Default()}
}

// ResetForTesting resets the singleton so that InitGlobal re-initialises on the
// next call. Must only be called from tests.
func ResetForTesting() {
	attachMu.Lock()
	defer attachMu.Unlock()
	once = sync.Once{}
	fallbackOnce = sync.Once{}
	instance.Store(nil)
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
	if l := instance.Load(); l != nil {
		return l.Shutdown(ctx)
	}
	return nil
}

// Deployment environment values that influence console formatting. Development
// uses a human-readable text handler; any other value uses a JSON handler.
const (
	EnvDevelopment = "dev"
	EnvProduction  = "prod"
)

type Config struct {
	ServiceName string
	Environment string     // deployment environment; EnvDevelopment selects text output
	EnableDebug bool       // legado: equivale a Level=Debug
	Level       slog.Level // nível do handler (zero = Info); EnableDebug tem precedência
	// RedactKeys extends DefaultRedactKeys: attributes whose key matches have
	// their value masked in the console and in every attached handler.
	RedactKeys []string
}

// Logger is the global logger: console output plus the handlers attached with
// Attach (the OTLP bridge installed by obs.Init, for instance).
type Logger struct {
	*slog.Logger
	console  slog.Handler
	level    slog.Level // minimum level, applied to the console and attached handlers
	redactor *redactor  // masks sensitive attributes, also for attached handlers
	service  string
	attached []slog.Handler
	closers  []func(context.Context) error
}

// New builds a console logger (text in development, JSON otherwise) and makes
// it slog's default. It exports nothing: exporters are attached with Attach, so
// this package does not link any OpenTelemetry SDK or gRPC code.
func New(_ context.Context, cfg Config) (*Logger, error) {
	level := cfg.Level
	if cfg.EnableDebug {
		level = slog.LevelDebug
	}
	r := newRedactor(cfg.RedactKeys)
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: r.replaceAttr}

	var consoleHandler slog.Handler
	if cfg.Environment == EnvDevelopment {
		consoleHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		consoleHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	l := &Logger{console: consoleHandler, level: level, redactor: r, service: cfg.ServiceName}
	l.rebuild()
	return l, nil
}

// Attach tees the global logger to h, keeping the console output, and makes
// the result slog's default. h receives only records at or above the level
// set in Config, like the console, so an exporter does not ship the Debug
// records the console drops, and with sensitive attributes masked (see
// Config.RedactKeys). shutdown, when not nil, runs in Shutdown so h can
// flush. Loggers derived (With, FromContext) before Attach keep writing only to
// the previous handlers. It returns ErrNotInitialized before InitGlobal.
func Attach(h slog.Handler, shutdown func(context.Context) error) error {
	attachMu.Lock()
	defer attachMu.Unlock()

	cur := instance.Load()
	if cur == nil {
		return ErrNotInitialized
	}
	r := cur.redactor
	if r == nil {
		r = newRedactor(nil)
	}
	next := &Logger{
		console:  cur.console,
		level:    cur.level,
		redactor: r,
		service:  cur.service,
		attached: append(slices.Clip(cur.attached), &levelHandler{
			Handler: &redactHandler{Handler: h, r: r},
			min:     cur.level,
		}),
		closers: cur.closers,
	}
	if shutdown != nil {
		next.closers = append(slices.Clip(cur.closers), shutdown)
	}
	next.rebuild()
	instance.Store(next)
	return nil
}

// rebuild assembles the slog.Logger from the console and attached handlers and
// installs it as slog's default.
func (l *Logger) rebuild() {
	h := l.console
	if len(l.attached) > 0 {
		h = &TeeHandler{handlers: append([]slog.Handler{l.console}, l.attached...)}
	}
	sl := slog.New(h)
	if l.service != "" {
		sl = sl.With("service", l.service)
	}
	slog.SetDefault(sl)
	l.Logger = sl
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

// Shutdown flushes the attached handlers, in reverse order of Attach.
func (l *Logger) Shutdown(ctx context.Context) error {
	var errs []error
	for _, closer := range slices.Backward(l.closers) {
		errs = append(errs, closer(ctx))
	}
	return errors.Join(errs...)
}

// --- levelHandler ---

// levelHandler drops records below min before they reach Handler.
type levelHandler struct {
	slog.Handler
	min slog.Level
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.Handler.Enabled(ctx, level)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithGroup(name), min: h.min}
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

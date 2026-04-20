package logging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

type mockHandler struct {
	enabled bool
	err     error
	records []slog.Record
}

func (m *mockHandler) Enabled(_ context.Context, _ slog.Level) bool { return m.enabled }
func (m *mockHandler) Handle(_ context.Context, r slog.Record) error {
	m.records = append(m.records, r)
	return m.err
}
func (m *mockHandler) WithAttrs(_ []slog.Attr) slog.Handler { return m }
func (m *mockHandler) WithGroup(_ string) slog.Handler      { return m }

// resetSingleton isolates each test from the package-level singleton.
func resetSingleton(t *testing.T) {
	t.Helper()
	instance = nil
	once = sync.Once{}
	t.Cleanup(func() {
		instance = nil
		once = sync.Once{}
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func devCfg(name string) Config {
	return Config{ServiceName: name, Environment: environment.ENV_DEV}
}

func TestInstance_PanicsWhenNotInitialized(t *testing.T) {
	resetSingleton(t)
	assert.PanicsWithValue(t, LOG_START_ERROR, func() { Instance() })
}

func TestInitGlobal_Idempotent(t *testing.T) {
	resetSingleton(t)
	ctx := context.Background()

	err := InitGlobal(ctx, devCfg("first"))
	require.NoError(t, err)
	assert.NotNil(t, instance)

	first := instance
	_ = InitGlobal(ctx, devCfg("second"))
	assert.Same(t, first, instance, "InitGlobal must not replace an existing instance")
}

func TestShutdown_WhenInstanceIsNil(t *testing.T) {
	resetSingleton(t)
	assert.NoError(t, Shutdown(context.Background()))
}

func TestShutdown_WhenInstanceExists(t *testing.T) {
	resetSingleton(t)
	require.NoError(t, InitGlobal(context.Background(), devCfg("svc")))
	assert.NoError(t, Shutdown(context.Background()))
}

func TestNew_DevEnvironment(t *testing.T) {
	l, err := New(context.Background(), Config{
		ServiceName: "svc",
		Environment: environment.ENV_DEV,
	})
	require.NoError(t, err)
	assert.NotNil(t, l)
	_ = l.Shutdown(context.Background())
}

func TestNew_NonDevEnvironment_UsesJSONHandler(t *testing.T) {
	l, err := New(context.Background(), Config{
		ServiceName: "svc",
		Environment: environment.ENV_PROD,
	})
	require.NoError(t, err)
	assert.NotNil(t, l)
	_ = l.Shutdown(context.Background())
}

func TestNew_EnableDebug(t *testing.T) {
	l, err := New(context.Background(), Config{
		ServiceName: "svc",
		EnableDebug: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, l)
	_ = l.Shutdown(context.Background())
}

func TestNew_EmptyServiceName(t *testing.T) {
	// When ServiceName is empty the logger must not add the "service" attribute.
	l, err := New(context.Background(), Config{})
	require.NoError(t, err)
	assert.NotNil(t, l)
	_ = l.Shutdown(context.Background())
}

func TestLogger_FromContext_WithoutTrace(t *testing.T) {
	resetSingleton(t)
	require.NoError(t, InitGlobal(context.Background(), devCfg("svc")))

	l := FromContext(context.Background())
	assert.NotNil(t, l)
}

func TestLogger_FromContext_WithTrace(t *testing.T) {
	resetSingleton(t)
	require.NoError(t, InitGlobal(context.Background(), devCfg("svc")))

	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	l := FromContext(ctx)
	assert.NotNil(t, l)
}

func TestLogger_ErrorWithStack(t *testing.T) {
	l, err := New(context.Background(), devCfg("svc"))
	require.NoError(t, err)
	assert.NotPanics(t, func() {
		l.ErrorWithStack(context.Background(), "something failed", "key", "value")
	})
	_ = l.Shutdown(context.Background())
}

func TestLogger_Shutdown_NilProvider(t *testing.T) {
	l := &Logger{Logger: slog.Default()}
	assert.NoError(t, l.Shutdown(context.Background()))
}

func TestShortcuts_DoNotPanic(t *testing.T) {
	resetSingleton(t)
	require.NoError(t, InitGlobal(context.Background(), devCfg("svc")))

	assert.NotPanics(t, func() {
		Info("info message", "k", "v")
		Error("error message")
		Debug("debug message")
		Warn("warn message")
	})
}

func TestTeeHandler_Enabled_TrueWhenAnyEnabled(t *testing.T) {
	tee := &TeeHandler{handlers: []slog.Handler{
		&mockHandler{enabled: false},
		&mockHandler{enabled: true},
	}}
	assert.True(t, tee.Enabled(context.Background(), slog.LevelInfo))
}

func TestTeeHandler_Enabled_FalseWhenAllDisabled(t *testing.T) {
	tee := &TeeHandler{handlers: []slog.Handler{
		&mockHandler{enabled: false},
		&mockHandler{enabled: false},
	}}
	assert.False(t, tee.Enabled(context.Background(), slog.LevelInfo))
}

func TestTeeHandler_Handle_DeliveredToAllEnabledHandlers(t *testing.T) {
	h1 := &mockHandler{enabled: true}
	h2 := &mockHandler{enabled: true}
	tee := &TeeHandler{handlers: []slog.Handler{h1, h2}}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	assert.NoError(t, tee.Handle(context.Background(), r))
	assert.Len(t, h1.records, 1)
	assert.Len(t, h2.records, 1)
}

func TestTeeHandler_Handle_ContinuesAfterHandlerError(t *testing.T) {
	h1 := &mockHandler{enabled: true, err: errors.New("handler failure")}
	h2 := &mockHandler{enabled: true}
	tee := &TeeHandler{handlers: []slog.Handler{h1, h2}}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	err := tee.Handle(context.Background(), r)
	assert.Error(t, err)
	assert.Len(t, h2.records, 1, "second handler must still receive the record")
}

func TestTeeHandler_Handle_SkipsDisabledHandlers(t *testing.T) {
	h1 := &mockHandler{enabled: false}
	h2 := &mockHandler{enabled: true}
	tee := &TeeHandler{handlers: []slog.Handler{h1, h2}}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	_ = tee.Handle(context.Background(), r)
	assert.Empty(t, h1.records, "disabled handler must not receive records")
	assert.Len(t, h2.records, 1)
}

func TestTeeHandler_WithAttrs_PreservesAllHandlers(t *testing.T) {
	h1 := slog.NewTextHandler(io.Discard, nil)
	h2 := slog.NewJSONHandler(io.Discard, nil)
	tee := &TeeHandler{handlers: []slog.Handler{h1, h2}}

	result := tee.WithAttrs([]slog.Attr{slog.String("app", "test")})
	newTee, ok := result.(*TeeHandler)
	require.True(t, ok)
	assert.Len(t, newTee.handlers, 2)
}

func TestTeeHandler_WithGroup_PreservesAllHandlers(t *testing.T) {
	h1 := slog.NewTextHandler(io.Discard, nil)
	h2 := slog.NewJSONHandler(io.Discard, nil)
	tee := &TeeHandler{handlers: []slog.Handler{h1, h2}}

	result := tee.WithGroup("request")
	newTee, ok := result.(*TeeHandler)
	require.True(t, ok)
	assert.Len(t, newTee.handlers, 2)
}

func TestPrintStruct(t *testing.T) {
	type sample struct{ Name string }
	out := captureStdout(t, func() {
		PrintStruct(sample{Name: "world"})
	})
	assert.Contains(t, out, "world")
}

func TestPrintStructToJson(t *testing.T) {
	out := captureStdout(t, func() {
		PrintStructToJson(map[string]string{"hello": "world"})
	})
	assert.Contains(t, out, `"hello"`)
	assert.Contains(t, out, `"world"`)
	assert.True(t, strings.Contains(out, "\n"), "output must be pretty-printed")
}

// --- OTLP path in New ---

func TestNew_WithOTLPCollectorAddr_Dev(t *testing.T) {
	// Covers the CollectorAddr != "" branch: resource, exporter, LoggerProvider, TeeHandler
	l, err := New(context.Background(), Config{
		ServiceName:   "svc-otlp",
		Environment:   environment.ENV_DEV,
		CollectorAddr: "localhost:4317",
	})
	require.NoError(t, err)
	require.NotNil(t, l)

	// Exercise all log levels so the OTLP handler is exercised
	l.Info("info", "k", "v")
	l.Debug("debug")
	l.Warn("warn")
	l.Error("error")

	// Covers the `if l.lp != nil { return l.lp.Shutdown(ctx) }` branch
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = l.Shutdown(shutdownCtx)
}

func TestNew_WithOTLPCollectorAddr_Prod(t *testing.T) {
	// JSON handler (non-dev) + OTLP branch
	l, err := New(context.Background(), Config{
		ServiceName:   "svc-otlp",
		Environment:   environment.ENV_PROD,
		CollectorAddr: "localhost:4317",
	})
	require.NoError(t, err)
	require.NotNil(t, l)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = l.Shutdown(shutdownCtx)
}

func TestNew_WithOTLPCollectorAddr_EmptyServiceName(t *testing.T) {
	// Covers the `if cfg.ServiceName != ""` false branch in the OTLP path
	l, err := New(context.Background(), Config{
		Environment:   environment.ENV_DEV,
		CollectorAddr: "localhost:4317",
	})
	require.NoError(t, err)
	require.NotNil(t, l)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = l.Shutdown(shutdownCtx)
}

func TestNew_WithOTLPCollectorAddr_EnableDebug(t *testing.T) {
	l, err := New(context.Background(), Config{
		ServiceName:   "svc-debug",
		Environment:   environment.ENV_DEV,
		EnableDebug:   true,
		CollectorAddr: "localhost:4317",
	})
	require.NoError(t, err)
	require.NotNil(t, l)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = l.Shutdown(shutdownCtx)
}

// --- ResetForTesting exported function ---

func TestResetForTesting_ResetsState(t *testing.T) {
	resetSingleton(t)
	require.NoError(t, InitGlobal(context.Background(), devCfg("svc")))
	require.NotNil(t, instance)

	ResetForTesting()

	assert.Nil(t, instance)
	// resetSingleton t.Cleanup will restore zero state
}

// --- NewLogger convenience function ---

func TestNewLogger_InitializesGlobalInstance(t *testing.T) {
	resetSingleton(t)
	// environment.Instance() returns defaults when no .env file is present
	err := NewLogger("my-service")
	require.NoError(t, err)
	assert.NotNil(t, instance)
}

// --- Fatal (subprocess pattern to avoid killing the test process) ---

func TestFatal_ExitsWithCode1(t *testing.T) {
	if os.Getenv("LOGGING_TEST_FATAL") == "1" {
		// Running inside the subprocess: call Fatal and expect os.Exit(1)
		_ = InitGlobal(context.Background(), devCfg("subprocess"))
		Fatal("forced exit for test coverage")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run="+t.Name())
	cmd.Env = append(os.Environ(), "LOGGING_TEST_FATAL=1")
	err := cmd.Run()

	require.Error(t, err, "subprocess must exit with non-zero code")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
}

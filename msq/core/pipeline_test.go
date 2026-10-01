package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/core"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// captureBroker hands the pipeline handler to the test and records sends.
type captureBroker struct {
	handlers chan port.MessageHandler
	mu       sync.Mutex
	sent     []*types.Message
}

func newCaptureBroker() *captureBroker {
	return &captureBroker{handlers: make(chan port.MessageHandler, 1)}
}

func (b *captureBroker) NewProducer() (port.Producer, error) { return &captureProducer{b: b}, nil }
func (b *captureBroker) NewConsumer(types.ConsumeConfig) (port.Consumer, error) {
	return &captureConsumer{b: b}, nil
}

func (b *captureBroker) messages() []*types.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*types.Message(nil), b.sent...)
}

type captureProducer struct{ b *captureBroker }

func (p *captureProducer) SendMessage(_ context.Context, m *types.Message) error {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	p.b.sent = append(p.b.sent, m)
	return nil
}
func (p *captureProducer) SendMessagesBatch(ctx context.Context, ms []*types.Message) error {
	for _, m := range ms {
		_ = p.SendMessage(ctx, m)
	}
	return nil
}
func (p *captureProducer) Close() error { return nil }

type captureConsumer struct{ b *captureBroker }

func (c *captureConsumer) Consume(ctx context.Context, h port.MessageHandler) error {
	c.b.handlers <- h
	<-ctx.Done()
	return nil
}
func (c *captureConsumer) Close() error  { return nil }
func (c *captureConsumer) Pause() error  { return nil }
func (c *captureConsumer) Resume() error { return nil }

type eventLog struct {
	mu     sync.Mutex
	events []types.BrokerEvent
}

func (l *eventLog) add(_ context.Context, e types.BrokerEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) types() []types.BrokerEventType {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]types.BrokerEventType, 0, len(l.events))
	for _, e := range l.events {
		out = append(out, e.Type)
	}
	return out
}

// env is a service over a captureBroker.
type env struct {
	svc *core.BrokerService
	b   *captureBroker
}

func newService(t *testing.T, events *eventLog) env {
	t.Helper()
	b := newCaptureBroker()
	cfg := core.ServiceConfig{Broker: b, System: "test"}
	if events != nil {
		cfg.OnEvent = events.add
	}
	return env{svc: core.NewService(cfg), b: b}
}

// pipeline starts a consumer and returns the handler the provider receives.
func (e env) pipeline(t *testing.T, cfg types.ConsumeConfig, h port.MessageHandler) port.MessageHandler {
	t.Helper()
	c, err := e.svc.NewConsumer(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Consume(ctx, h) }()
	t.Cleanup(func() { cancel(); <-done; _ = c.Close() })
	return <-e.b.handlers
}

func msg(t *testing.T) *types.Message {
	t.Helper()
	m, err := types.NewMessageWithTopic("orders", "payload")
	require.NoError(t, err)
	return m
}

func TestPipeline_RetriesNackUntilAck(t *testing.T) {
	e := newService(t, nil)
	calls := 0
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 3, RetryBackoff: time.Millisecond},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			calls++
			if calls < 3 {
				return types.Nack, errors.New("temporary")
			}
			return types.Ack, nil
		}))

	res, err := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Ack, res)
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestPipeline_NackWithoutErrorRetries(t *testing.T) {
	e := newService(t, nil)
	calls := 0
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 2, RetryBackoff: time.Millisecond},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			calls++
			return types.Nack, nil
		}))

	res, _ := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Nack, res)
	assert.Equal(t, 3, calls, "initial attempt + MaxRetries")
}

func TestPipeline_IgnoreIsNotRetried(t *testing.T) {
	e := newService(t, nil)
	calls := 0
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 3, RetryBackoff: time.Millisecond},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			calls++
			return types.Ignore, nil
		}))

	res, _ := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Ignore, res)
	assert.Equal(t, 1, calls)
}

func TestPipeline_DeadLettersAfterRetries(t *testing.T) {
	events := &eventLog{}
	e := newService(t, events)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 1, RetryBackoff: time.Millisecond, DeadLetterTopic: "orders.dlq"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("boom")
		}))

	m := msg(t).WithHeader("tenant", "a")
	res, err := h.Handle(context.Background(), m)
	assert.Equal(t, types.Ack, res, "dead-lettered messages are acked on the source")
	assert.Error(t, err)

	sent := e.b.messages()
	require.Len(t, sent, 1)
	dead := sent[0]
	assert.Equal(t, "orders.dlq", dead.Topic)
	assert.Equal(t, m.Id, dead.Id)
	assert.Equal(t, "orders", dead.Headers[core.HeaderDLQOriginalTopic])
	assert.Equal(t, "boom", dead.Headers[core.HeaderDLQError])
	assert.Equal(t, "2", dead.Headers[core.HeaderDLQAttempts])
	assert.Equal(t, "a", dead.Headers["tenant"])
	assert.Equal(t, "orders", m.Topic, "the source message is not mutated")
	assert.Contains(t, events.types(), types.EventMessageDeadLettered)
}

func TestPipeline_NackWithoutDLQIsReturned(t *testing.T) {
	e := newService(t, nil)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("boom")
		}))

	res, err := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Nack, res)
	assert.EqualError(t, err, "boom")
	assert.Empty(t, e.b.messages())
}

func TestPipeline_PanicBecomesNack(t *testing.T) {
	e := newService(t, nil)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			panic("bad input")
		}))

	res, err := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Nack, res)
	assert.ErrorContains(t, err, "bad input")
}

func TestPipeline_HandlerSurvivesShutdown(t *testing.T) {
	e := newService(t, nil)
	var handlerErr error
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(ctx context.Context, _ *types.Message) (types.Result, error) {
			handlerErr = ctx.Err()
			return types.Ack, nil
		}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown already started
	res, _ := h.Handle(ctx, msg(t))
	assert.Equal(t, types.Ack, res)
	assert.NoError(t, handlerErr, "in-flight handlers must drain, not fail")
}

func TestPipeline_ShutdownStopsRetryWait(t *testing.T) {
	e := newService(t, nil)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 5, RetryBackoff: time.Hour},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("down")
		}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, _ := h.Handle(ctx, msg(t))
	assert.Equal(t, types.Nack, res)
	assert.Less(t, time.Since(start), time.Second)
}

func TestPipeline_HandlerTimeout(t *testing.T) {
	e := newService(t, nil)
	var hasDeadline bool
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", HandlerTimeout: time.Minute},
		port.MessageHandlerFunc(func(ctx context.Context, _ *types.Message) (types.Result, error) {
			_, hasDeadline = ctx.Deadline()
			return types.Ack, nil
		}))

	_, _ = h.Handle(context.Background(), msg(t))
	assert.True(t, hasDeadline)
}

func TestPipeline_Events(t *testing.T) {
	events := &eventLog{}
	e := newService(t, events)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Ack, nil
		}))

	_, _ = h.Handle(context.Background(), msg(t))
	assert.Equal(t, []types.BrokerEventType{types.EventConsumerStarted, types.EventMessageReceived, types.EventMessageAcked}, events.types())
}

func TestPipeline_PropagatesTraceFromProducer(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	e := newService(t, nil)
	p, err := e.svc.NewProducer()
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), msg(t)))
	sent := e.b.messages()
	require.Len(t, sent, 1)
	require.NotEmpty(t, sent[0].Headers["traceparent"])

	var consumerSpan trace.SpanContext
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(ctx context.Context, _ *types.Message) (types.Result, error) {
			consumerSpan = trace.SpanContextFromContext(ctx)
			return types.Ack, nil
		}))
	_, _ = h.Handle(context.Background(), sent[0])

	spans := rec.Ended()
	require.Len(t, spans, 2)
	send, process := spans[0], spans[1]
	assert.Equal(t, "send orders", send.Name())
	assert.Equal(t, trace.SpanKindProducer, send.SpanKind())
	assert.Equal(t, "process orders", process.Name())
	assert.Equal(t, trace.SpanKindConsumer, process.SpanKind())
	assert.Equal(t, send.SpanContext().TraceID(), process.SpanContext().TraceID())
	assert.Equal(t, send.SpanContext().SpanID(), process.Parent().SpanID())
	assert.Equal(t, process.SpanContext().SpanID(), consumerSpan.SpanID(), "handler runs inside the process span")
}

func TestNewConsumerManager_WrapsPlainBroker(t *testing.T) {
	b := newCaptureBroker()
	m := core.NewConsumerManager(b)
	m.Register(types.ConsumeConfig{Topic: "orders"}, func(context.Context, *types.Message) (types.Result, error) {
		panic("boom")
	})
	require.NoError(t, m.Start())
	t.Cleanup(m.Close)

	h := <-b.handlers
	res, err := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Nack, res, "plain brokers also get panic recovery")
	assert.Error(t, err)
}

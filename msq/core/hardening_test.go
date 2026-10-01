package core_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/msq/core"
	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// flakyBroker fails NewConsumer createFails times, then returns consumers
// whose Consume fails consumeFails times before blocking until ctx ends.
type flakyBroker struct {
	fakeBroker
	createFails  atomic.Int32
	consumeFails atomic.Int32
	created      atomic.Int32
	consuming    atomic.Int32
}

func (b *flakyBroker) NewConsumer(types.ConsumeConfig) (port.Consumer, error) {
	if b.createFails.Add(-1) >= 0 {
		return nil, errors.New("broker unreachable")
	}
	b.created.Add(1)
	return &flakyConsumer{b: b}, nil
}

type flakyConsumer struct{ b *flakyBroker }

func (c *flakyConsumer) Consume(ctx context.Context, _ port.MessageHandler) error {
	if c.b.consumeFails.Add(-1) >= 0 {
		return errors.New("create consumer group: transient")
	}
	c.b.consuming.Add(1)
	defer c.b.consuming.Add(-1)
	<-ctx.Done()
	return nil
}
func (c *flakyConsumer) Close() error  { return nil }
func (c *flakyConsumer) Pause() error  { return nil }
func (c *flakyConsumer) Resume() error { return nil }

func TestConsumerManager_RestartsFailedConsumer(t *testing.T) {
	b := &flakyBroker{}
	b.consumeFails.Store(1)
	events := &eventLog{}
	svc := core.NewService(core.ServiceConfig{Broker: b, OnEvent: events.add})
	m := svc.NewConsumerManager()
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "orders"}, ack)
	require.NoError(t, m.Start())

	require.Eventually(t, func() bool { return b.consuming.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"a consumer whose Consume fails must be recreated and restarted")
	assert.Equal(t, int32(2), b.created.Load())
	assert.Contains(t, events.types(), types.EventConsumerRestarting)
	assert.NoError(t, m.Healthy())
}

func TestConsumerManager_RetriesCreationFailure(t *testing.T) {
	b := &flakyBroker{}
	b.createFails.Store(1)
	m := core.NewConsumerManager(b)
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "orders"}, ack)
	err := m.Start()
	require.ErrorIs(t, err, core.ErrConsumerFailed, "Start still reports the failure")
	require.Eventually(t, func() bool { return b.consuming.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the failed consumer is retried in the background")
}

func TestConsumerManager_HealthyReportsConsumerDown(t *testing.T) {
	b := &flakyBroker{}
	b.consumeFails.Store(1 << 20) // never recovers
	m := core.NewConsumerManager(b).SetHealthGrace(50 * time.Millisecond)
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "orders"}, ack)
	require.NoError(t, m.Start())

	require.Eventually(t, func() bool { return m.Healthy() != nil }, 5*time.Second, 10*time.Millisecond)
	err := m.Healthy()
	assert.ErrorIs(t, err, core.ErrConsumerDown)
	assert.ErrorContains(t, err, "orders")
}

func TestConsumerManager_HealthyWithinGrace(t *testing.T) {
	b := &flakyBroker{}
	b.consumeFails.Store(1 << 20)
	m := core.NewConsumerManager(b) // default grace: 30s
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "orders"}, ack)
	require.NoError(t, m.Start())
	time.Sleep(50 * time.Millisecond)
	assert.NoError(t, m.Healthy(), "a restart within the grace is not reported")
}

func TestBrokerService_HealthyAggregatesManagers(t *testing.T) {
	b := &flakyBroker{}
	b.consumeFails.Store(1 << 20)
	svc := core.NewService(core.ServiceConfig{Broker: b})
	m := svc.NewConsumerManager().SetHealthGrace(0)
	defer m.Close()
	m.Register(types.ConsumeConfig{Topic: "orders"}, ack)
	require.NoError(t, m.Start())
	require.Eventually(t, func() bool { return errors.Is(svc.Healthy(), core.ErrConsumerDown) }, 5*time.Second, 10*time.Millisecond)
}

func TestConsumerManager_StartAfterClose(t *testing.T) {
	m := core.NewConsumerManager(&countingBroker{})
	m.Register(types.ConsumeConfig{Topic: "a"}, ack)
	require.NoError(t, m.Start())
	m.Close()

	m.Register(types.ConsumeConfig{Topic: "b"}, ack)
	assert.NotPanics(t, func() {
		assert.ErrorIs(t, m.Start(), core.ErrManagerClosed)
	})
	assert.ErrorIs(t, m.Healthy(), core.ErrManagerClosed)
}

// blockingBroker hands one message to a handler that ignores its context.
type blockingBroker struct {
	fakeBroker
	release chan struct{}
	entered chan struct{}
}

func (b *blockingBroker) NewConsumer(types.ConsumeConfig) (port.Consumer, error) {
	return &blockingConsumer{b: b}, nil
}

type blockingConsumer struct{ b *blockingBroker }

func (c *blockingConsumer) Consume(ctx context.Context, h port.MessageHandler) error {
	m, _ := types.NewMessageWithTopic("orders", 1)
	_, _ = h.Handle(ctx, m)
	return nil
}
func (c *blockingConsumer) Close() error  { return nil }
func (c *blockingConsumer) Pause() error  { return nil }
func (c *blockingConsumer) Resume() error { return nil }

func TestConsumerManager_CloseContextHonorsDeadline(t *testing.T) {
	b := &blockingBroker{release: make(chan struct{}), entered: make(chan struct{})}
	svc := core.NewService(core.ServiceConfig{Broker: b})
	m := svc.NewConsumerManager()
	m.Register(types.ConsumeConfig{Topic: "orders", HandlerTimeout: -1}, func(context.Context, *types.Message) (types.Result, error) {
		close(b.entered)
		<-b.release // stuck handler, ignores ctx
		return types.Ack, nil
	})
	require.NoError(t, m.Start())
	<-b.entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := svc.CloseContext(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)

	close(b.release)
	assert.NoError(t, m.CloseContext(context.Background()), "a later call waits for the same drain")
}

func TestPipeline_DefaultHandlerTimeout(t *testing.T) {
	e := newService(t, nil)
	var deadline time.Time
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"},
		port.MessageHandlerFunc(func(ctx context.Context, _ *types.Message) (types.Result, error) {
			deadline, _ = ctx.Deadline()
			return types.Ack, nil
		}))
	_, _ = h.Handle(context.Background(), msg(t))
	require.False(t, deadline.IsZero(), "a handler must never run unbounded by default")
	assert.WithinDuration(t, time.Now().Add(types.DefaultHandlerTimeout), deadline, 5*time.Second)
}

func TestService_DefaultsFillZeroFields(t *testing.T) {
	b := newCaptureBroker()
	svc := core.NewService(core.ServiceConfig{Broker: b, Defaults: core.ConsumeDefaults{MaxDeliveries: 2, HandlerTimeout: time.Minute}})
	var deadline time.Time
	c, err := svc.NewConsumer(types.ConsumeConfig{Topic: "orders"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, port.MessageHandlerFunc(func(ctx context.Context, _ *types.Message) (types.Result, error) {
			deadline, _ = ctx.Deadline()
			return types.Nack, errors.New("poison")
		}))
	}()
	t.Cleanup(func() { cancel(); <-done })
	h := <-b.handlers
	m := msg(t)
	m.DeliveryCount = 2
	res, _ := h.Handle(context.Background(), m)
	assert.Equal(t, types.Reject, res, "service MaxDeliveries default applies")
	assert.WithinDuration(t, time.Now().Add(time.Minute), deadline, 5*time.Second)
}

func TestPipeline_DeliveryLimit(t *testing.T) {
	events := &eventLog{}
	e := newService(t, events)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxDeliveries: 3},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("poison")
		}))

	m := msg(t)
	m.DeliveryCount = 2
	res, _ := h.Handle(context.Background(), m)
	assert.Equal(t, types.Nack, res, "below the limit the broker redelivers")

	m.DeliveryCount = 3
	res, err := h.Handle(context.Background(), m)
	assert.Equal(t, types.Reject, res, "the last delivery is rejected")
	assert.EqualError(t, err, "poison")
	assert.Contains(t, events.types(), types.EventMessageRejected)
	assert.Empty(t, e.b.messages())
}

func TestPipeline_DeliveryLimitUnknownOrUnlimited(t *testing.T) {
	e := newService(t, nil)
	nack := port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		return types.Nack, nil
	})
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders"}, nack)
	res, _ := h.Handle(context.Background(), msg(t)) // DeliveryCount 0: unknown
	assert.Equal(t, types.Nack, res)

	e2 := newService(t, nil)
	h = e2.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxDeliveries: -1}, nack)
	m := msg(t)
	m.DeliveryCount = 1000
	res, _ = h.Handle(context.Background(), m)
	assert.Equal(t, types.Nack, res, "negative MaxDeliveries is unlimited")
}

func TestPipeline_DeliveryLimitWithDLQDeadLetters(t *testing.T) {
	e := newService(t, nil)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxDeliveries: 1, DeadLetterTopic: "orders.dlq"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("poison")
		}))
	m := msg(t)
	m.DeliveryCount = 4
	res, _ := h.Handle(context.Background(), m)
	assert.Equal(t, types.Ack, res)
	sent := e.b.messages()
	require.Len(t, sent, 1)
	assert.Equal(t, "4", sent[0].Headers[core.HeaderDLQDeliveries])
	assert.Zero(t, sent[0].DeliveryCount)
}

func TestPipeline_DLQErrorIsTruncated(t *testing.T) {
	e := newService(t, nil)
	long := strings.Repeat("é", 1000) // 2 bytes per rune
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", DeadLetterTopic: "orders.dlq"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New(long)
		}))
	_, _ = h.Handle(context.Background(), msg(t))
	sent := e.b.messages()
	require.Len(t, sent, 1)
	got := sent[0].Headers[core.HeaderDLQError]
	assert.LessOrEqual(t, len(got), 512)
	assert.True(t, strings.HasPrefix(long, got))
	assert.True(t, len(got) > 500, "keeps as much as fits")
}

// A spoofed topic in the payload must not reach span names or metric labels.
func TestPipeline_TelemetryUsesConfiguredTopic(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	events := &eventLog{}
	e := newService(t, events)
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", DeadLetterTopic: "orders.dlq"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			return types.Nack, errors.New("x")
		}))
	m := msg(t)
	m.Topic = "orders.eu.123" // concrete subject set by the transport
	_, _ = h.Handle(context.Background(), m)

	spans := rec.Ended()
	require.NotEmpty(t, spans)
	assert.Equal(t, "process orders", spans[len(spans)-1].Name())
	for _, ev := range events.events {
		if ev.Type == types.EventMessageReceived {
			assert.Equal(t, "orders", ev.Topic)
		}
	}
	sent := e.b.messages()
	require.Len(t, sent, 1)
	assert.Equal(t, "orders.eu.123", sent[0].Headers[core.HeaderDLQOriginalTopic])
}

func TestProducer_DoesNotMutateCallerHeaders(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	e := newService(t, nil)
	p, err := e.svc.NewProducer()
	require.NoError(t, err)
	m := msg(t).WithHeader("tenant", "a")
	before := len(m.Headers)

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			ctx, span := otel.Tracer("t").Start(context.Background(), "op")
			defer span.End()
			assert.NoError(t, p.SendMessage(ctx, m))
			assert.NoError(t, p.SendMessagesBatch(ctx, []*types.Message{m}))
		})
	}
	wg.Wait()
	assert.Len(t, m.Headers, before, "the caller's message must not be mutated")
	sent := e.b.messages()
	require.Len(t, sent, 32)
	assert.Equal(t, "a", sent[0].Headers["tenant"])
	assert.NotEmpty(t, sent[0].Headers["traceparent"], "the copy carries the trace context")
	assert.NotSame(t, m, sent[0])
}

func TestPipeline_HandlerRejectIsReported(t *testing.T) {
	events := &eventLog{}
	e := newService(t, events)
	calls := 0
	h := e.pipeline(t, types.ConsumeConfig{Topic: "orders", MaxRetries: 3, DeadLetterTopic: "orders.dlq"},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			calls++
			return types.Reject, errors.New("malformed")
		}))
	res, _ := h.Handle(context.Background(), msg(t))
	assert.Equal(t, types.Reject, res, "a handler's Reject is neither retried nor dead-lettered")
	assert.Equal(t, 1, calls)
	assert.Empty(t, e.b.messages())
	assert.Contains(t, events.types(), types.EventMessageRejected)
}

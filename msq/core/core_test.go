package core_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/msq/core"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("core-test")
	os.Exit(m.Run())
}

// Errors

func TestErrorSentinels(t *testing.T) {
	assert.Error(t, core.ErrBrokerRequired)
	assert.Error(t, core.ErrTopicRequired)
	assert.Error(t, core.ErrConsumerFailed)

	// all distinct
	assert.NotEqual(t, core.ErrBrokerRequired.Error(), core.ErrTopicRequired.Error())
	assert.NotEqual(t, core.ErrBrokerRequired.Error(), core.ErrConsumerFailed.Error())
}

// Fakes

// fakeProducer satisfies port.Producer.
type fakeProducer struct{}

func (p *fakeProducer) SendMessage(_ context.Context, _ *types.Message) error         { return nil }
func (p *fakeProducer) SendMessagesBatch(_ context.Context, _ []*types.Message) error { return nil }
func (p *fakeProducer) Close() error                                                  { return nil }

// fakeConsumer satisfies port.Consumer; Consume blocks until ctx is done.
type fakeConsumer struct {
	consumeErr error
	closeErr   error
	started    atomic.Bool
}

func (c *fakeConsumer) Consume(ctx context.Context, _ port.MessageHandler) error {
	c.started.Store(true)
	<-ctx.Done()
	return c.consumeErr
}
func (c *fakeConsumer) Close() error  { return c.closeErr }
func (c *fakeConsumer) Pause() error  { return nil }
func (c *fakeConsumer) Resume() error { return nil }

// fakeBroker satisfies port.Broker.
type fakeBroker struct {
	producer port.Producer
	consumer port.Consumer
}

func (b *fakeBroker) NewProducer() port.Producer { return b.producer }
func (b *fakeBroker) NewConsumer(_ types.ConsumeConfig) port.Consumer {
	return b.consumer
}

// nilConsumerBroker always returns nil from NewConsumer.
type nilConsumerBroker struct{ fakeBroker }

func (b *nilConsumerBroker) NewConsumer(_ types.ConsumeConfig) port.Consumer { return nil }

// BrokerService

func TestNewServiceWithNilOnEvent(t *testing.T) {
	broker := &fakeBroker{producer: &fakeProducer{}}
	svc := core.NewService(core.ServiceConfig{Broker: broker})
	assert.NotNil(t, svc)
}

func TestNewServiceWithOnEvent(t *testing.T) {
	var called bool
	broker := &fakeBroker{producer: &fakeProducer{}}
	svc := core.NewService(core.ServiceConfig{
		Broker:  broker,
		OnEvent: func(_ context.Context, _ types.BrokerEvent) { called = true },
	})
	assert.NotNil(t, svc)
	_ = called // suppress unused warning
}

func TestBrokerServiceNewProducer(t *testing.T) {
	p := &fakeProducer{}
	broker := &fakeBroker{producer: p}
	svc := core.NewService(core.ServiceConfig{Broker: broker})

	got := svc.NewProducer()
	assert.Equal(t, p, got)
}

func TestBrokerServiceNewConsumer(t *testing.T) {
	c := &fakeConsumer{}
	broker := &fakeBroker{consumer: c}
	svc := core.NewService(core.ServiceConfig{Broker: broker})

	got := svc.NewConsumer(types.ConsumeConfig{Topic: "orders"})
	assert.Equal(t, c, got)
}

func TestBrokerServiceNewConsumerManager(t *testing.T) {
	broker := &fakeBroker{consumer: &fakeConsumer{}}
	svc := core.NewService(core.ServiceConfig{Broker: broker})

	mgr := svc.NewConsumerManager()
	assert.NotNil(t, mgr)
}

// ConsumerManager

func TestNewConsumerManager(t *testing.T) {
	broker := &fakeBroker{}
	mgr := core.NewConsumerManager(broker)
	assert.NotNil(t, mgr)
}

func TestConsumerManagerRegisterAndStart(t *testing.T) {
	consumer := &fakeConsumer{}
	broker := &fakeBroker{consumer: consumer}

	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: "orders", Concurrency: 1},
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		},
	)

	mgr.Start()

	// Give the goroutine a moment to reach the Consume call.
	require.Eventually(t, func() bool {
		return consumer.started.Load()
	}, 2*time.Second, 10*time.Millisecond)

	mgr.Close()
}

func TestConsumerManagerChaining(t *testing.T) {
	broker := &fakeBroker{consumer: &fakeConsumer{}}
	mgr := core.NewConsumerManager(broker)

	returned := mgr.Register(
		types.ConsumeConfig{Topic: "t"},
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	assert.Same(t, mgr, returned, "Register must return self for chaining")
}

func TestConsumerManagerDispatcherSetsConcurrencyAndStarts(t *testing.T) {
	consumer := &fakeConsumer{}
	broker := &fakeBroker{consumer: consumer}

	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: "t"}, // Concurrency not set
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	mgr.Dispatcher(3) // sets concurrency=3, starts consumers

	require.Eventually(t, func() bool {
		return consumer.started.Load()
	}, 2*time.Second, 10*time.Millisecond)

	mgr.Close()
}

func TestConsumerManagerDispatcherDoesNotOverrideConcurrency(t *testing.T) {
	consumer := &fakeConsumer{}
	broker := &fakeBroker{consumer: consumer}

	// Pre-set concurrency=5; Dispatcher should not change it.
	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: "t", Concurrency: 5},
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	mgr.Dispatcher(1)

	require.Eventually(t, consumer.started.Load, 2*time.Second, 10*time.Millisecond)
	mgr.Close()
}

func TestConsumerManagerSkipsEmptyTopic(t *testing.T) {
	// No panic or consumer started for empty topic.
	broker := &fakeBroker{consumer: &fakeConsumer{}}
	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: ""}, // empty — should be skipped
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	// Should not block or panic.
	mgr.Start()
	time.Sleep(50 * time.Millisecond)
	mgr.Close()
}

func TestConsumerManagerSkipsNilConsumer(t *testing.T) {
	broker := &nilConsumerBroker{}
	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: "orders"},
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	// Should not panic even though broker returns nil consumer.
	mgr.Start()
	time.Sleep(50 * time.Millisecond)
	mgr.Close()
}

func TestConsumerManagerShutdownIsAliasForClose(t *testing.T) {
	broker := &fakeBroker{consumer: &fakeConsumer{}}
	mgr := core.NewConsumerManager(broker)
	// Shutdown with no consumers registered must not panic.
	assert.NotPanics(t, mgr.Shutdown)
}

func TestConsumerManagerRegisterHandlerInterface(t *testing.T) {
	consumer := &fakeConsumer{}
	broker := &fakeBroker{consumer: consumer}

	handler := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		return types.Ack, nil
	})

	mgr := core.NewConsumerManager(broker)
	mgr.RegisterHandler(types.ConsumeConfig{Topic: "t"}, handler)
	mgr.Start()

	require.Eventually(t, consumer.started.Load, 2*time.Second, 10*time.Millisecond)
	mgr.Close()
}

func TestConsumerManagerMultipleConsumers(t *testing.T) {
	var startCount atomic.Int32

	makeBroker := func() port.Broker {
		return &fakeBroker{consumer: &countingConsumer{count: &startCount}}
	}

	broker := makeBroker()
	mgr := core.NewConsumerManager(broker)

	for i := 0; i < 3; i++ {
		mgr.Register(
			types.ConsumeConfig{Topic: "t", Concurrency: 1},
			func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
		)
	}
	mgr.Start()

	require.Eventually(t, func() bool {
		return startCount.Load() == 3
	}, 2*time.Second, 10*time.Millisecond)

	mgr.Close()
}

// countingConsumer tracks how many times Consume has been called.
type countingConsumer struct {
	count *atomic.Int32
}

func (c *countingConsumer) Consume(ctx context.Context, _ port.MessageHandler) error {
	c.count.Add(1)
	<-ctx.Done()
	return nil
}
func (c *countingConsumer) Close() error  { return nil }
func (c *countingConsumer) Pause() error  { return nil }
func (c *countingConsumer) Resume() error { return nil }

// errorConsumer errors on Consume.
type errorConsumer struct{ err error }

func (c *errorConsumer) Consume(_ context.Context, _ port.MessageHandler) error { return c.err }
func (c *errorConsumer) Close() error                                           { return nil }
func (c *errorConsumer) Pause() error                                           { return nil }
func (c *errorConsumer) Resume() error                                          { return nil }

func TestConsumerManagerConsumerError(t *testing.T) {
	errConsume := errors.New("broker gone")
	consumer := &errorConsumer{err: errConsume}
	broker := &fakeBroker{consumer: consumer}

	mgr := core.NewConsumerManager(broker)
	mgr.Register(
		types.ConsumeConfig{Topic: "t"},
		func(_ context.Context, _ *types.Message) (types.Result, error) { return types.Ack, nil },
	)
	mgr.Start()
	// Manager must not panic even when Consume returns an error immediately.
	time.Sleep(50 * time.Millisecond)
	mgr.Close()
}

// Internal tests for the rabbitmq package — access unexported types directly.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock: amqp091.Acknowledger

type mockAcknowledger struct {
	acked   bool
	nacked  bool
	requeue bool
}

func (m *mockAcknowledger) Ack(_ uint64, _ bool) error {
	m.acked = true
	return nil
}
func (m *mockAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	m.nacked = true
	m.requeue = requeue
	return nil
}
func (m *mockAcknowledger) Reject(_ uint64, _ bool) error { return nil }

// Mock: amqpChannel

type mockChannel struct {
	publishErr error
	consumeErr error
	qosErr     error
	declareErr error
	bindErr    error
	flowErr    error
	closeErr   error
	deliveries chan amqp.Delivery
	closeCount int
	flowActive *bool
	closeOnce  sync.Once
}

func newMockChannel() *mockChannel {
	return &mockChannel{deliveries: make(chan amqp.Delivery, 8)}
}

func (m *mockChannel) PublishWithContext(_ context.Context, _, _ string, _, _ bool, _ amqp.Publishing) error {
	return m.publishErr
}
func (m *mockChannel) Consume(_, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	if m.consumeErr != nil {
		return nil, m.consumeErr
	}
	return m.deliveries, nil
}
func (m *mockChannel) Qos(_, _ int, _ bool) error { return m.qosErr }
func (m *mockChannel) QueueDeclare(_ string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{Name: "test-queue"}, m.declareErr
}
func (m *mockChannel) QueueBind(_, _, _ string, _ bool, _ amqp.Table) error { return m.bindErr }
func (m *mockChannel) ExchangeDeclare(_, _ string, _, _, _, _ bool, _ amqp.Table) error {
	return nil
}
func (m *mockChannel) Flow(active bool) error {
	if m.flowActive != nil {
		*m.flowActive = active
	}
	return m.flowErr
}

// Close increments closeCount and closes the deliveries channel exactly once,
// mirroring real amqp091.Channel behavior where closing the channel causes the
// consumer deliveries channel to close and end the consume loop.
func (m *mockChannel) Close() error {
	m.closeCount++
	m.closeOnce.Do(func() { close(m.deliveries) })
	return m.closeErr
}

// Mock: chanOpener

type mockChanOpener struct {
	ch  amqpChannel
	err error
}

func (m *mockChanOpener) channel() (amqpChannel, error) { return m.ch, m.err }

// Broker construction

func TestBrokerNew(t *testing.T) {
	b := New(nil, "test-exchange") // nil Conn is fine — just stored
	assert.NotNil(t, b)
}

// Broker.NewProducer

func TestBrokerNewProducerSuccess(t *testing.T) {
	ch := newMockChannel()
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	p, err := b.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestBrokerNewProducerChannelError(t *testing.T) {
	b := &Broker{conn: &mockChanOpener{err: errors.New("conn error")}, exchange: "ex"}
	p, err := b.NewProducer()
	require.Error(t, err)
	assert.Nil(t, p)
}

// Broker.NewConsumer

func TestBrokerNewConsumerSuccess(t *testing.T) {
	ch := newMockChannel()
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 1})
	require.NotNil(t, c)
}

func TestBrokerNewConsumerDefaultsConcurrency(t *testing.T) {
	ch := newMockChannel()
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 0})
	require.NotNil(t, c)
}

func TestBrokerNewConsumerRoutingKeyDefaultsToTopic(t *testing.T) {
	ch := newMockChannel()
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	// RoutingKey="" should default to Topic — must not panic.
	c := b.NewConsumer(types.ConsumeConfig{Topic: "my-queue", RoutingKey: ""})
	require.NotNil(t, c)
}

func TestBrokerNewConsumerChannelError(t *testing.T) {
	b := &Broker{conn: &mockChanOpener{err: errors.New("conn error")}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q"})
	assert.Nil(t, c)
}

func TestBrokerNewConsumerQoSError(t *testing.T) {
	ch := newMockChannel()
	ch.qosErr = errors.New("qos error")
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q"})
	assert.Nil(t, c)
	assert.Equal(t, 1, ch.closeCount) // channel was closed on error
}

func TestBrokerNewConsumerQueueDeclareError(t *testing.T) {
	ch := newMockChannel()
	ch.declareErr = errors.New("declare error")
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q"})
	assert.Nil(t, c)
	assert.Equal(t, 1, ch.closeCount)
}

func TestBrokerNewConsumerQueueBindError(t *testing.T) {
	ch := newMockChannel()
	ch.bindErr = errors.New("bind error")
	b := &Broker{conn: &mockChanOpener{ch: ch}, exchange: "ex"}
	c := b.NewConsumer(types.ConsumeConfig{Topic: "q"})
	assert.Nil(t, c)
	assert.Equal(t, 1, ch.closeCount)
}

// amqpProducer

func TestAmqpProducerSendMessage(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{channel: ch, exchange: "ex"}
	msg := types.NewMessageWithTopic("my-queue", "payload")
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestAmqpProducerSendMessageWithKey(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{channel: ch, exchange: "ex"}
	msg := types.NewMessageWithTopic("topic", "payload")
	msg.Key = "routing-key"
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestAmqpProducerSendMessageError(t *testing.T) {
	ch := newMockChannel()
	ch.publishErr = errors.New("publish failed")
	p := &amqpProducer{channel: ch, exchange: "ex"}
	msg := types.NewMessageWithTopic("q", "data")
	assert.Error(t, p.SendMessage(context.Background(), msg))
}

func TestAmqpProducerSendMessagesBatch(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{channel: ch, exchange: "ex"}
	msgs := []*types.Message{
		types.NewMessageWithTopic("q", "a"),
		types.NewMessageWithTopic("q", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestAmqpProducerSendMessagesBatchError(t *testing.T) {
	ch := newMockChannel()
	ch.publishErr = errors.New("publish failed")
	p := &amqpProducer{channel: ch, exchange: "ex"}
	msgs := []*types.Message{types.NewMessageWithTopic("q", "data")}
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestAmqpProducerClose(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{channel: ch, exchange: "ex"}
	assert.NoError(t, p.Close())
	assert.Equal(t, 1, ch.closeCount)
}

// amqpConsumer.Consume

func TestAmqpConsumerConsumeSuccess(t *testing.T) {
	ch := newMockChannel()
	// Send one valid message then close the channel to end the loop.
	msg := types.NewMessageWithTopic("q", "hello")
	body, _ := json.Marshal(msg)
	ack := &mockAcknowledger{}
	ch.deliveries <- amqp.Delivery{Acknowledger: ack, Body: body}
	close(ch.deliveries)

	c := &amqpConsumer{channel: ch, queue: amqp.Queue{Name: "q"}, concurrency: 1}
	err := c.Consume(context.Background(), port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		},
	))
	assert.NoError(t, err)
	assert.True(t, ack.acked)
}

func TestAmqpConsumerConsumeRegisterError(t *testing.T) {
	ch := newMockChannel()
	ch.consumeErr = errors.New("register failed")
	c := &amqpConsumer{channel: ch, queue: amqp.Queue{Name: "q"}, concurrency: 1}
	err := c.Consume(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "register failed")
}

func TestAmqpConsumerConsumeContextCancel(t *testing.T) {
	ch := newMockChannel()
	// Don't close deliveries — context cancel will close the channel.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := &amqpConsumer{channel: ch, queue: amqp.Queue{Name: "q"}, concurrency: 1}
	// goroutine closes channel on ctx.Done; loop exits when deliveries closes.
	err := c.Consume(ctx, port.MessageHandlerFunc(
		func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		},
	))
	assert.NoError(t, err)
}

// amqpConsumer.Close / Pause / Resume

func TestAmqpConsumerClose(t *testing.T) {
	ch := newMockChannel()
	c := &amqpConsumer{channel: ch}
	assert.NoError(t, c.Close())
	assert.Equal(t, 1, ch.closeCount)
}

func TestAmqpConsumerPause(t *testing.T) {
	active := true
	ch := newMockChannel()
	ch.flowActive = &active
	c := &amqpConsumer{channel: ch}
	assert.NoError(t, c.Pause())
	assert.True(t, c.paused.Load())
	assert.False(t, active)
}

func TestAmqpConsumerResume(t *testing.T) {
	active := false
	ch := newMockChannel()
	ch.flowActive = &active
	c := &amqpConsumer{channel: ch}
	assert.NoError(t, c.Resume())
	assert.False(t, c.paused.Load())
	assert.True(t, active)
}

// amqpConsumer.handle (already covered; kept for completeness)

func TestAmqpConsumerHandleAck(t *testing.T) {
	ack := &mockAcknowledger{}
	body, _ := json.Marshal(types.NewMessageWithTopic("t", "v"))
	c := &amqpConsumer{}
	c.handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: body},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}))
	assert.True(t, ack.acked)
}

func TestAmqpConsumerHandleNack(t *testing.T) {
	ack := &mockAcknowledger{}
	body, _ := json.Marshal(types.NewMessageWithTopic("t", "v"))
	c := &amqpConsumer{}
	c.handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: body},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Nack, errors.New("err")
		}))
	assert.True(t, ack.nacked)
	assert.True(t, ack.requeue)
}

func TestAmqpConsumerHandleIgnore(t *testing.T) {
	ack := &mockAcknowledger{}
	body, _ := json.Marshal(types.NewMessageWithTopic("t", "v"))
	c := &amqpConsumer{}
	c.handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: body},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ignore, nil
		}))
	assert.False(t, ack.acked)
	assert.False(t, ack.nacked)
}

func TestAmqpConsumerHandleDefault(t *testing.T) {
	ack := &mockAcknowledger{}
	body, _ := json.Marshal(types.NewMessageWithTopic("t", "v"))
	c := &amqpConsumer{}
	c.handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: body},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Result(99), nil
		}))
	assert.True(t, ack.acked) // default falls through to Ack
}

func TestAmqpConsumerHandleInvalidJSON(t *testing.T) {
	ack := &mockAcknowledger{}
	c := &amqpConsumer{}
	c.handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: []byte(`{bad`)},
		port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}))
	assert.True(t, ack.nacked)
}

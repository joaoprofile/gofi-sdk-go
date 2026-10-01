// Internal tests for the rabbitmq package — access unexported types directly.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock: amqp091.Acknowledger

type mockAcknowledger struct {
	acked, nacked, requeue bool
}

func (m *mockAcknowledger) Ack(_ uint64, _ bool) error { m.acked = true; return nil }
func (m *mockAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	m.nacked, m.requeue = true, requeue
	return nil
}
func (m *mockAcknowledger) Reject(_ uint64, _ bool) error { return nil }

// handleDelivery runs one delivery through a consumer with a tracker.
func handleDelivery(c *amqpConsumer, d amqp.Delivery, res types.Result) (*mockAcknowledger, *types.Message) {
	ack := &mockAcknowledger{}
	d.Acknowledger = ack
	var got *types.Message
	c.handle(context.Background(), &d, port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		got = m
		return res, nil
	}))
	return ack, got
}

// Mock: amqpChannel

type mockChannel struct {
	publishErr error
	confirmErr error // returned by the confirm wait
	modeErr    error // returned by Confirm (confirm mode)
	consumeErr error
	qosErr     error
	declareErr error
	bindErr    error
	exchangeFn func(name string) error
	passiveErr error // returned by ExchangeDeclarePassive

	passiveQueue bool // QueueDeclarePassive was called

	mu          sync.Mutex
	deliveries  chan amqp.Delivery
	closed      bool
	closeCount  int
	confirming  bool
	canceledTag string
	published   []amqp.Publishing
	keys        []string
}

func newMockChannel() *mockChannel {
	return &mockChannel{deliveries: make(chan amqp.Delivery, 8)}
}

func (m *mockChannel) Publish(_ context.Context, _, key string, msg amqp.Publishing) (func(context.Context) error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	m.published = append(m.published, msg)
	m.keys = append(m.keys, key)
	confirmErr := m.confirmErr
	return func(context.Context) error { return confirmErr }, nil
}

func (m *mockChannel) Confirm(bool) error {
	m.confirming = m.modeErr == nil
	return m.modeErr
}

func (m *mockChannel) Consume(_, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	if m.consumeErr != nil {
		return nil, m.consumeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deliveries, nil
}

// Cancel mirrors amqp091: the consumer's deliveries channel is closed; a new
// Consume gets a fresh one.
func (m *mockChannel) Cancel(consumer string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.canceledTag = consumer
	close(m.deliveries)
	m.deliveries = make(chan amqp.Delivery, 8)
	return nil
}

func (m *mockChannel) push(d amqp.Delivery) {
	m.mu.Lock()
	ch := m.deliveries
	m.mu.Unlock()
	ch <- d
}

func (m *mockChannel) Qos(_, _ int, _ bool) error { return m.qosErr }
func (m *mockChannel) QueueDeclare(name string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{Name: name}, m.declareErr
}
func (m *mockChannel) QueueDeclarePassive(name string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passiveQueue = true
	return amqp.Queue{Name: name}, nil
}
func (m *mockChannel) QueueBind(_, _, _ string, _ bool, _ amqp.Table) error { return m.bindErr }
func (m *mockChannel) ExchangeDeclare(name, _ string, _, _, _, _ bool, _ amqp.Table) error {
	if m.exchangeFn != nil {
		return m.exchangeFn(name)
	}
	return nil
}

func (m *mockChannel) ExchangeDeclarePassive(_, _ string, _, _, _, _ bool, _ amqp.Table) error {
	return m.passiveErr
}

func (m *mockChannel) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// Close mirrors amqp091: closing the channel ends the deliveries stream.
func (m *mockChannel) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCount++
	if !m.closed {
		m.closed = true
		close(m.deliveries)
	}
	return nil
}

// Mock: chanOpener returning channels in order (the last one repeats).

type mockChanOpener struct {
	mu    sync.Mutex
	chans []*mockChannel
	err   error
	opens atomic.Int32
}

func openerOf(chans ...*mockChannel) *mockChanOpener { return &mockChanOpener{chans: chans} }

func (m *mockChanOpener) channel() (amqpChannel, error) {
	m.opens.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := m.chans[0]
	if len(m.chans) > 1 {
		m.chans = m.chans[1:]
	}
	return ch, nil
}

func ackHandler(seen chan<- string) port.MessageHandler {
	return port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		seen <- m.Topic
		return types.Ack, nil
	})
}

func delivery(topic string) amqp.Delivery {
	b, _ := json.Marshal(testMessageWithTopic(topic, "v"))
	return amqp.Delivery{Body: b, RoutingKey: topic, Acknowledger: &mockAcknowledger{}}
}

// Broker

func TestBrokerSetupDeclaresMissingExchange(t *testing.T) {
	var declared string
	check, create := newMockChannel(), newMockChannel()
	check.passiveErr = &amqp.Error{Code: amqp.NotFound, Reason: "NOT_FOUND"}
	create.exchangeFn = func(name string) error { declared = name; return nil }
	b := &Broker{conn: openerOf(check, create), exchange: "orders"}

	require.NoError(t, b.Setup(context.Background()))
	assert.Equal(t, "orders", declared)
	assert.True(t, create.IsClosed(), "the setup channel is released")
}

// An existing exchange of any kind (topic, fanout, ...) is left untouched.
func TestBrokerSetupKeepsExistingExchange(t *testing.T) {
	ch := newMockChannel()
	ch.exchangeFn = func(string) error { t.Fatal("must not redeclare an existing exchange"); return nil }
	b := &Broker{conn: openerOf(ch), exchange: "orders"}
	require.NoError(t, b.Setup(context.Background()))
}

func TestBrokerSetupDefaultExchangeIsNoop(t *testing.T) {
	o := openerOf(newMockChannel())
	b := &Broker{conn: o}
	require.NoError(t, b.Setup(context.Background()))
	assert.Zero(t, o.opens.Load())
}

func TestBrokerSetupError(t *testing.T) {
	check, create := newMockChannel(), newMockChannel()
	check.passiveErr = &amqp.Error{Code: amqp.NotFound}
	create.exchangeFn = func(string) error { return errors.New("access refused") }
	b := &Broker{conn: openerOf(check, create), exchange: "orders"}
	assert.ErrorContains(t, b.Setup(context.Background()), "access refused")

	denied := newMockChannel()
	denied.passiveErr = &amqp.Error{Code: amqp.AccessRefused}
	assert.ErrorContains(t, (&Broker{conn: openerOf(denied), exchange: "orders"}).Setup(context.Background()), "check failed")
}

func TestBrokerImplementsSetupAndCloser(t *testing.T) {
	var _ port.BrokerSetup = (*Broker)(nil)
	assert.NoError(t, New(nil, "").Close())
}

// Producer

func TestBrokerNewProducerUsesConfirmMode(t *testing.T) {
	ch := newMockChannel()
	b := &Broker{conn: openerOf(ch), exchange: "ex"}
	_, err := b.NewProducer()
	require.NoError(t, err)
	assert.True(t, ch.confirming)
}

func TestBrokerNewProducerErrors(t *testing.T) {
	b := &Broker{conn: &mockChanOpener{err: errors.New("conn error")}}
	_, err := b.NewProducer()
	assert.Error(t, err)

	ch := newMockChannel()
	ch.modeErr = errors.New("confirm not supported")
	_, err = (&Broker{conn: openerOf(ch)}).NewProducer()
	assert.ErrorContains(t, err, "confirm")
	assert.True(t, ch.IsClosed())
}

// Key is the portable ordering key, not a routing key: routing by it sent
// keyed messages (and their dead-letter copies) to no queue.
func TestProducerRoutesByTopicAndCarriesKey(t *testing.T) {
	for _, enc := range []types.Encoding{types.EncodingEnvelope, types.EncodingCloudEvents} {
		ch := newMockChannel()
		p := &amqpProducer{opener: openerOf(ch), exchange: "ex", encoding: enc}
		require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "v")))
		require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "v").WithKey("customer-7")))
		assert.Equal(t, []string{"orders", "orders"}, ch.keys, string(enc))
		got := decode(&amqp.Delivery{Body: ch.published[1].Body, Headers: ch.published[1].Headers, RoutingKey: "orders"})
		assert.Equal(t, "customer-7", got.Key, string(enc))
	}
}

func TestProducerRequiresTopic(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex"}
	assert.Error(t, p.SendMessage(context.Background(), (&types.Message{}).WithKey("k")))
	assert.Empty(t, ch.keys)
}

func TestProducerBatchAwaitsSentConfirmsOnPublishError(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex"}
	err := p.SendMessagesBatch(context.Background(), []*types.Message{
		testMessageWithTopic("orders", 1), {}, testMessageWithTopic("orders", 3),
	})
	assert.ErrorContains(t, err, "must be set")
	assert.Equal(t, []string{"orders"}, ch.keys, "the batch stops at the failing message")
}

func TestProducerReturnsBrokerNack(t *testing.T) {
	ch := newMockChannel()
	ch.confirmErr = errors.New("rabbitmq: publish nacked by broker")
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex"}
	assert.ErrorContains(t, p.SendMessage(context.Background(), testMessageWithTopic("q", "v")), "nacked")
}

func TestProducerPublishError(t *testing.T) {
	ch := newMockChannel()
	ch.publishErr = errors.New("publish failed")
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex"}
	assert.Error(t, p.SendMessage(context.Background(), testMessageWithTopic("q", "v")))
	assert.Error(t, p.SendMessagesBatch(context.Background(), []*types.Message{testMessageWithTopic("q", "v")}))
}

func TestProducerReopensLostChannel(t *testing.T) {
	lost, fresh := newMockChannel(), newMockChannel()
	p := &amqpProducer{opener: openerOf(lost, fresh), exchange: "ex"}
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("q", "a")))

	lost.Close() // connection dropped
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("q", "b")))
	assert.Len(t, lost.published, 1)
	assert.Len(t, fresh.published, 1)
	assert.True(t, fresh.confirming)
}

func TestProducerRetriesOnceOnErrClosed(t *testing.T) {
	stale, fresh := newMockChannel(), newMockChannel()
	stale.publishErr = amqp.ErrClosed // closed but not yet flagged
	p := &amqpProducer{opener: openerOf(stale, fresh), exchange: "ex"}
	_, _ = p.channel()
	stale.mu.Lock()
	stale.closed = true
	stale.mu.Unlock()

	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("q", "v")))
	assert.Len(t, fresh.published, 1)
}

func TestProducerBatchWaitsForEveryConfirm(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex"}
	msgs := []*types.Message{testMessageWithTopic("q", "a"), testMessageWithTopic("q", "b")}
	require.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
	assert.Len(t, ch.published, 2)

	ch.confirmErr = errors.New("nacked")
	assert.Error(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestProducerClose(t *testing.T) {
	assert.NoError(t, (&amqpProducer{}).Close())
	ch := newMockChannel()
	p := &amqpProducer{opener: openerOf(ch)}
	_, _ = p.channel()
	assert.NoError(t, p.Close())
	assert.True(t, ch.IsClosed())
}

// Consumer construction

func TestBrokerNewConsumerDeclaresAndBinds(t *testing.T) {
	ch := newMockChannel()
	c, err := (&Broker{conn: openerOf(ch), exchange: "ex"}).NewConsumer(types.ConsumeConfig{Topic: "q"})
	require.NoError(t, err)
	ac := c.(*amqpConsumer)
	assert.Equal(t, types.DefaultConcurrency, ac.concurrency)
	_, queue := ac.current()
	assert.Equal(t, "q", queue)
}

// An existing queue with other arguments (e.g. a quorum queue) is used as is.
func TestBrokerNewConsumerUsesExistingQueueWithOtherArguments(t *testing.T) {
	first, second := newMockChannel(), newMockChannel()
	first.declareErr = &amqp.Error{Code: amqp.PreconditionFailed, Reason: "inequivalent arg 'x-queue-type'"}
	c, err := (&Broker{conn: openerOf(first, second), exchange: "ex"}).NewConsumer(types.ConsumeConfig{Topic: "q"})
	require.NoError(t, err)
	assert.Equal(t, 1, first.closeCount)
	assert.True(t, second.passiveQueue)
	_, queue := c.(*amqpConsumer).current()
	assert.Equal(t, "q", queue)
}

func TestBrokerNewConsumerErrorsCloseTheChannel(t *testing.T) {
	for name, set := range map[string]func(*mockChannel){
		"qos":     func(m *mockChannel) { m.qosErr = errors.New("x") },
		"declare": func(m *mockChannel) { m.declareErr = errors.New("x") },
		"bind":    func(m *mockChannel) { m.bindErr = errors.New("x") },
	} {
		t.Run(name, func(t *testing.T) {
			ch := newMockChannel()
			set(ch)
			c, err := (&Broker{conn: openerOf(ch), exchange: "ex"}).NewConsumer(types.ConsumeConfig{Topic: "q"})
			assert.Nil(t, c)
			assert.Error(t, err)
			assert.Equal(t, 1, ch.closeCount)
		})
	}
	_, err := (&Broker{conn: &mockChanOpener{err: errors.New("down")}}).NewConsumer(types.ConsumeConfig{Topic: "q"})
	assert.Error(t, err)
}

// Consume loop

func newConsumer(t *testing.T, o *mockChanOpener) *amqpConsumer {
	t.Helper()
	c, err := (&Broker{conn: o, exchange: "ex"}).NewConsumer(types.ConsumeConfig{Topic: "q", Concurrency: 1})
	require.NoError(t, err)
	return c.(*amqpConsumer)
}

func run(c *amqpConsumer, h port.MessageHandler) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Consume(ctx, h) }()
	return cancel, done
}

func TestConsumeDeliversAndStopsOnCancel(t *testing.T) {
	ch := newMockChannel()
	c := newConsumer(t, openerOf(ch))
	seen := make(chan string, 1)
	cancel, done := run(c, ackHandler(seen))

	ch.push(delivery("hello"))
	assert.Equal(t, "hello", <-seen)
	cancel()
	assert.NoError(t, <-done)
}

func TestConsumeRegisterErrorOnOpenChannel(t *testing.T) {
	ch := newMockChannel()
	c := newConsumer(t, openerOf(ch))
	ch.consumeErr = errors.New("register failed")
	assert.ErrorContains(t, c.Consume(context.Background(), ackHandler(nil)), "register failed")
}

// A dropped channel is re-opened and consumption continues.
func TestConsumeReopensLostChannel(t *testing.T) {
	first, second := newMockChannel(), newMockChannel()
	o := openerOf(first, second)
	c := newConsumer(t, o)
	seen := make(chan string, 2)
	cancel, done := run(c, ackHandler(seen))
	defer func() { cancel(); <-done }()

	first.push(delivery("before"))
	assert.Equal(t, "before", <-seen)

	first.Close() // broker dropped the channel
	second.push(delivery("after"))
	select {
	case got := <-seen:
		assert.Equal(t, "after", got)
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not reopen the channel")
	}
	assert.Equal(t, int32(2), o.opens.Load())
}

func TestConsumePauseResume(t *testing.T) {
	ch := newMockChannel()
	c := newConsumer(t, openerOf(ch))
	seen := make(chan string, 2)
	cancel, done := run(c, ackHandler(seen))

	ch.push(delivery("before"))
	assert.Equal(t, "before", <-seen)
	require.NoError(t, c.Pause())
	assert.Equal(t, c.tag, ch.canceledTag)
	assert.True(t, c.gate.Paused())

	require.NoError(t, c.Resume())
	ch.push(delivery("after"))
	select {
	case got := <-seen:
		assert.Equal(t, "after", got)
	case <-time.After(time.Second):
		t.Fatal("consumer did not resume")
	}
	cancel()
	assert.NoError(t, <-done)
}

func TestConsumerClose(t *testing.T) {
	ch := newMockChannel()
	c := newConsumer(t, openerOf(ch))
	assert.NoError(t, c.Close())
	assert.True(t, ch.IsClosed())
}

// handle / decode

func handleWith(t *testing.T, body []byte, res types.Result, err error) (*mockAcknowledger, *types.Message) {
	t.Helper()
	ack := &mockAcknowledger{}
	var got *types.Message
	(&amqpConsumer{}).handle(context.Background(), &amqp.Delivery{Acknowledger: ack, Body: body},
		port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			got = m
			return res, err
		}))
	return ack, got
}

func TestHandleResults(t *testing.T) {
	body, _ := json.Marshal(testMessageWithTopic("t", "v"))

	ack, _ := handleWith(t, body, types.Ack, nil)
	assert.True(t, ack.acked)

	ack, _ = handleWith(t, body, types.Nack, nil)
	assert.True(t, ack.nacked)
	assert.True(t, ack.requeue, "every Nack requeues; Ignore is the discard")

	ack, _ = handleWith(t, body, types.Ignore, nil)
	assert.True(t, ack.acked)

	ack, _ = handleWith(t, body, types.Result(99), nil)
	assert.True(t, ack.acked)

	ack, _ = handleWith(t, body, types.Reject, nil)
	assert.True(t, ack.nacked)
	assert.False(t, ack.requeue, "Reject goes to the queue's DLX or is dropped, never requeued")
}

// Quorum queues count deliveries in x-delivery-count (previous deliveries).
func TestHandleDeliveryCountFromQuorumHeader(t *testing.T) {
	c := &amqpConsumer{deliveries: newDeliveryTracker(), cfg: types.ConsumeConfig{RetryBackoff: time.Millisecond}}
	body, _ := json.Marshal(testMessageWithTopic("t", "v"))
	_, got := handleDelivery(c, amqp.Delivery{Body: body, Headers: amqp.Table{"x-delivery-count": int64(4)}, Redelivered: true}, types.Ack)
	assert.Equal(t, 5, got.DeliveryCount)
	_, got = handleDelivery(c, amqp.Delivery{Body: body}, types.Ack)
	assert.Equal(t, 1, got.DeliveryCount)
}

// Classic queues only flag Redelivered: the consumer counts by message-id.
func TestHandleDeliveryCountTrackedLocally(t *testing.T) {
	c := &amqpConsumer{deliveries: newDeliveryTracker(), cfg: types.ConsumeConfig{RetryBackoff: time.Millisecond}}
	d := amqp.Delivery{Body: []byte(`raw`), MessageId: "m-1", RoutingKey: "orders"}
	var ids []string
	for want := 1; want <= 3; want++ {
		ack, got := handleDelivery(c, d, types.Nack)
		assert.Equal(t, want, got.DeliveryCount)
		assert.True(t, ack.requeue)
		ids = append(ids, got.Id.String())
		d.Redelivered = true
	}
	assert.Equal(t, ids[0], ids[2], "a foreign message keeps its Id (from message-id) across redeliveries")
	_, _ = handleDelivery(c, d, types.Reject)
	assert.Empty(t, c.deliveries.nacks, "a settled message is forgotten")
}

// The transport routing key wins over the topic in the payload.
func TestHandleTopicFromTransport(t *testing.T) {
	c := &amqpConsumer{deliveries: newDeliveryTracker(), cfg: types.ConsumeConfig{Topic: "orders"}}
	body, _ := json.Marshal(testMessageWithTopic("spoofed-"+strings.Repeat("x", 10), "v"))
	_, got := handleDelivery(c, amqp.Delivery{Body: body, RoutingKey: "orders.created"}, types.Ack)
	assert.Equal(t, "orders.created", got.Topic)
	_, got = handleDelivery(c, amqp.Delivery{Body: body}, types.Ack)
	assert.Equal(t, "orders", got.Topic)
}

// Bodies from other producers reach the handler raw instead of being dropped.
func TestHandleForeignBody(t *testing.T) {
	ack, got := handleWith(t, []byte(`{bad`), types.Ack, nil)
	require.NotNil(t, got)
	assert.Equal(t, `{bad`, string(got.Value))
	assert.True(t, ack.acked)
}

func TestCloudEventsRoundTrip(t *testing.T) {
	ch := newMockChannel()
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex", encoding: types.EncodingCloudEvents}
	in := testMessageWithTopic("orders", "v").WithHeader("tenant", "a")
	require.NoError(t, p.SendMessage(context.Background(), in))

	pub := ch.published[0]
	assert.Equal(t, "1.0", pub.Headers["cloudEvents_specversion"])
	assert.Equal(t, in.Id.String(), pub.MessageId)
	assert.JSONEq(t, string(in.Value), string(pub.Body), "binary mode sends the bare payload")

	out := decode(&amqp.Delivery{Body: pub.Body, Headers: pub.Headers, RoutingKey: "orders"})
	assert.Equal(t, in.Id, out.Id)
	assert.Equal(t, "orders", out.Topic)
	assert.Equal(t, map[string]string{"tenant": "a"}, out.Headers)
}

func TestEnvelopeIsDefault(t *testing.T) {
	ch := newMockChannel()
	b := New(nil, "ex")
	p := &amqpProducer{opener: openerOf(ch), exchange: "ex", encoding: b.encoding}
	in := testMessageWithTopic("orders", "v")
	require.NoError(t, p.SendMessage(context.Background(), in))

	out := decode(&amqp.Delivery{Body: ch.published[0].Body})
	assert.Equal(t, in.Id, out.Id)
	assert.Equal(t, "orders", out.Topic)
	assert.Nil(t, ch.published[0].Headers)
}

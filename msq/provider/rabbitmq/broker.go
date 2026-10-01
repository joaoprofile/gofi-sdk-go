package rabbitmq

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
)

// amqpChannel is the subset of *amqp091.Channel the broker uses (see realChannel).
type amqpChannel interface {
	Publish(ctx context.Context, exchange, key string, msg amqp091.Publishing) (wait func(context.Context) error, err error)
	Confirm(noWait bool) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp091.Table) (<-chan amqp091.Delivery, error)
	Qos(prefetchCount, prefetchSize int, global bool) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp091.Table) (amqp091.Queue, error)
	QueueDeclarePassive(name string, durable, autoDelete, exclusive, noWait bool, args amqp091.Table) (amqp091.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp091.Table) error
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp091.Table) error
	ExchangeDeclarePassive(name, kind string, durable, autoDelete, internal, noWait bool, args amqp091.Table) error
	Cancel(consumer string, noWait bool) error
	IsClosed() bool
	Close() error
}

// chanOpener abstracts *Conn so that the Broker can work without a real AMQP connection.
type chanOpener interface {
	channel() (amqpChannel, error)
}

// Broker implements port.Broker for RabbitMQ.
type Broker struct {
	conn     chanOpener
	closer   func() error
	exchange string
	encoding types.Encoding
}

// Option configures a Broker.
type Option func(*Broker)

// WithEncoding selects the wire format producers write; consumers read both.
// The default is types.EncodingEnvelope.
func WithEncoding(e types.Encoding) Option {
	return func(b *Broker) { b.encoding = e }
}

// New creates a Broker bound to the given connection and exchange.
func New(conn *Conn, exchange string, opts ...Option) *Broker {
	b := &Broker{conn: conn, exchange: exchange, encoding: types.EncodingEnvelope}
	if conn != nil {
		b.closer = conn.Close
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Setup declares the exchange (durable, direct); the default exchange ("")
// needs none. gofi's builder calls it during Build.
func (b *Broker) Setup(context.Context) error {
	if b.exchange == "" {
		return nil
	}
	return declareExchange(b.conn, b.exchange)
}

// Close closes the connection.
func (b *Broker) Close() error {
	if b.closer == nil {
		return nil
	}
	return b.closer()
}

// NewProducer opens a channel in confirm mode: SendMessage returns only after
// the broker has taken responsibility for the message. Messages are published
// as mandatory, so one no queue is bound for fails with ErrUnroutable instead
// of being dropped: declare the queues (start the consumers) before producing.
func (b *Broker) NewProducer() (port.Producer, error) {
	p := &amqpProducer{opener: b.conn, exchange: b.exchange, encoding: b.encoding}
	if _, err := p.channel(); err != nil {
		logging.Error("rabbitmq: failed to open producer channel", slog.Any("error", err))
		return nil, fmt.Errorf("rabbitmq: open producer channel: %w", err)
	}
	return p, nil
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	routingKey := cfg.RoutingKey
	if routingKey == "" {
		routingKey = cfg.Topic
	}

	// open declares a durable classic queue when it is missing. A queue that
	// already exists with other arguments (quorum type, dead-letter exchange,
	// ...) makes the declare fail with PRECONDITION_FAILED: it is then used
	// as it is, like an existing exchange.
	open := func() (amqpChannel, amqp091.Queue, error) {
		ch, err := b.consumerChannel(concurrency)
		if err != nil {
			return nil, amqp091.Queue{}, err
		}
		q, err := ch.QueueDeclare(cfg.Topic, true, false, false, false, nil)
		if amqpErr, ok := errors.AsType[*amqp091.Error](err); ok && amqpErr.Code == amqp091.PreconditionFailed {
			ch.Close() // the failed declare closed it on the broker
			if ch, err = b.consumerChannel(concurrency); err != nil {
				return nil, amqp091.Queue{}, err
			}
			q, err = ch.QueueDeclarePassive(cfg.Topic, true, false, false, false, nil)
		}
		if err != nil {
			ch.Close()
			return nil, amqp091.Queue{}, fmt.Errorf("rabbitmq: declare queue %q: %w", cfg.Topic, err)
		}
		if err := ch.QueueBind(cfg.Topic, routingKey, b.exchange, false, nil); err != nil {
			ch.Close()
			return nil, amqp091.Queue{}, fmt.Errorf("rabbitmq: bind queue %q: %w", cfg.Topic, err)
		}
		return ch, q, nil
	}
	ch, q, err := open()
	if err != nil {
		return nil, err
	}
	return &amqpConsumer{
		open:        open,
		channel:     ch,
		queue:       q,
		cfg:         cfg,
		concurrency: concurrency,
		tag:         "gofi-" + uuid.NewString(),
		deliveries:  newDeliveryTracker(),
	}, nil
}

// consumerChannel opens a channel with the consumer's prefetch.
func (b *Broker) consumerChannel(prefetch int) (amqpChannel, error) {
	ch, err := b.conn.channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: open consumer channel: %w", err)
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("rabbitmq: qos: %w", err)
	}
	return ch, nil
}

// Producer

type amqpProducer struct {
	opener   chanOpener
	exchange string
	encoding types.Encoding

	mu sync.Mutex
	ch amqpChannel
}

// channel returns the confirm-mode channel, re-opening it after a loss.
func (p *amqpProducer) channel() (amqpChannel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	ch, err := p.opener.channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("rabbitmq producer: confirm mode: %w", err)
	}
	p.ch = ch
	return ch, nil
}

func (p *amqpProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	wait, err := p.publish(ctx, msg)
	if err != nil {
		return err
	}
	return wait(ctx)
}

// SendMessagesBatch publishes every message, then waits for all confirms. A
// publish error stops the batch; the confirms of what was sent are still
// awaited so their results are reported too.
func (p *amqpProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	waits := make([]func(context.Context) error, 0, len(msgs))
	var errs []error
	for _, msg := range msgs {
		wait, err := p.publish(ctx, msg)
		if err != nil {
			errs = append(errs, err)
			break
		}
		waits = append(waits, wait)
	}
	for _, wait := range waits {
		if err := wait(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// publish routes by Topic and retries once on a fresh channel when the
// current one was lost. Key is not a routing key (it is the portable ordering
// key): it travels in the envelope or the CloudEvents partitionkey.
func (p *amqpProducer) publish(ctx context.Context, msg *types.Message) (func(context.Context) error, error) {
	if msg.Topic == "" {
		return nil, errors.New("rabbitmq producer: msg.Topic (routing key) must be set")
	}
	pub, err := p.publishing(msg)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		ch, err := p.channel()
		if err != nil {
			return nil, fmt.Errorf("rabbitmq producer: %w", err)
		}
		wait, err := ch.Publish(ctx, p.exchange, msg.Topic, pub)
		if err == nil {
			return wait, nil
		}
		if attempt > 0 || !errors.Is(err, amqp091.ErrClosed) {
			return nil, fmt.Errorf("rabbitmq producer: publish: %w", err)
		}
	}
}

func (p *amqpProducer) publishing(msg *types.Message) (amqp091.Publishing, error) {
	pub := amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		MessageId:    msg.Id.String(),
		Timestamp:    msg.Timestamp,
	}
	if p.encoding == types.EncodingCloudEvents {
		body, headers := types.AMQPBinding.Encode(msg)
		pub.Body = body
		pub.Headers = make(amqp091.Table, len(headers))
		for k, v := range headers {
			pub.Headers[k] = v
		}
		return pub, nil
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return pub, fmt.Errorf("rabbitmq producer: marshal failed: %w", err)
	}
	pub.Body = body
	return pub, nil
}

func (p *amqpProducer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch == nil {
		return nil
	}
	return p.ch.Close()
}

// Consumer

type amqpConsumer struct {
	open        func() (amqpChannel, amqp091.Queue, error)
	cfg         types.ConsumeConfig
	concurrency int
	tag         string // consumer tag; lets Pause cancel this consumer only
	gate        worker.Gate
	canceled    atomic.Bool // set by Pause so Consume can tell a pause from channel loss
	deliveries  *deliveryTracker

	mu      sync.Mutex
	channel amqpChannel
	queue   amqp091.Queue
}

func (c *amqpConsumer) current() (amqpChannel, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channel, c.queue.Name
}

// Consume delivers messages until ctx ends. A lost channel (or connection)
// is re-opened with backoff; unacked messages are redelivered by the broker.
func (c *amqpConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	pool := worker.New(c.concurrency)
	defer pool.Close()

	stop := context.AfterFunc(ctx, func() {
		ch, _ := c.current()
		ch.Close()
	})
	defer stop()

	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		ch, queue := c.current()
		deliveries, err := ch.Consume(queue, c.tag, false, false, false, false, nil)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if !ch.IsClosed() {
				return fmt.Errorf("rabbitmq consumer: register failed: %w", err)
			}
		} else {
			for d := range deliveries {
				pool.Enqueue(func() { c.handle(ctx, &d, handler) })
			}
			if ctx.Err() != nil {
				return nil
			}
			if c.canceled.Swap(false) { // paused
				if c.gate.Wait(ctx) != nil {
					return nil
				}
				continue
			}
		}
		logging.Warn("rabbitmq consumer: channel lost, reopening", slog.String("queue", c.cfg.Topic))
		if !c.reopen(ctx, &backoff) {
			return nil
		}
	}
}

// reopen replaces the channel; it reports false when ctx ended first.
func (c *amqpConsumer) reopen(ctx context.Context, backoff *worker.Backoff) bool {
	for {
		if worker.Sleep(ctx, backoff.Next()) != nil {
			return false
		}
		ch, q, err := c.open()
		if err != nil {
			logging.Warn("rabbitmq consumer: reopen failed", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
			continue
		}
		c.mu.Lock()
		c.channel, c.queue = ch, q
		c.mu.Unlock()
		backoff.Reset()
		logging.Info("rabbitmq consumer: channel reopened", slog.String("queue", c.cfg.Topic))
		return true
	}
}

// Bounds of the wait before a nacked message is requeued, so a failing
// message does not spin between broker and consumer.
const (
	requeueDelayMin = time.Second
	requeueDelayMax = 10 * time.Second
)

func (c *amqpConsumer) handle(ctx context.Context, d *amqp091.Delivery, handler port.MessageHandler) {
	msg := decode(d)
	msg.Topic = cmp.Or(d.RoutingKey, c.cfg.Topic, msg.Topic)
	msg.DeliveryCount = c.deliveries.count(d)
	result, err := handler.Handle(ctx, &msg)

	switch result {
	case types.Nack:
		logging.Error("rabbitmq consumer: handler nacked, requeueing",
			slog.String("queue", c.cfg.Topic), slog.Int("deliveries", msg.DeliveryCount), slog.Any("error", err))
		c.deliveries.nacked(d, msg.DeliveryCount)
		// Shutdown skips the wait: the message is requeued at once.
		_ = worker.Sleep(ctx, worker.RedeliveryDelay(cmp.Or(c.cfg.RetryBackoff, requeueDelayMin), requeueDelayMax, msg.DeliveryCount))
		d.Nack(false, true)
	case types.Reject:
		// No requeue: a queue with a dead-letter exchange (x-dead-letter-exchange
		// argument or policy) routes it there; otherwise the broker drops it.
		c.deliveries.settled(d)
		d.Nack(false, false)
	default:
		// Ack = processed, Ignore = discarded on purpose. Both must ack: an
		// unacked delivery holds a prefetch slot until the channel closes,
		// then gets requeued.
		c.deliveries.settled(d)
		d.Ack(false)
	}
}

// decode reads CloudEvents binary messages, gofi envelopes and, from other
// producers, the raw body. A message without an Id gets one from the AMQP
// message-id, so it keeps it across redeliveries.
func decode(d *amqp091.Delivery) types.Message {
	headers := make(map[string]string, len(d.Headers))
	for k, v := range d.Headers {
		if s, ok := v.(string); ok {
			headers[k] = s
		} else {
			headers[k] = fmt.Sprint(v)
		}
	}
	var m types.Message
	if types.AMQPBinding.IsBinary(headers) {
		m = types.AMQPBinding.Decode(d.Body, headers)
		if m.Topic == "" {
			m.Topic = d.RoutingKey
		}
	} else {
		m = types.DecodeEnvelope(d.Body)
		if len(headers) > 0 && m.Headers == nil {
			m.Headers = headers
		}
	}
	if m.Id == uuid.Nil {
		if d.MessageId != "" {
			m.Id = types.StableID(d.MessageId)
		} else {
			m.Id = uuid.New() // nothing stable to derive it from
		}
	}
	return m
}

// maxTrackedDeliveries bounds the local redelivery counts; beyond it they are
// forgotten and counting restarts, which only delays the delivery limit.
const maxTrackedDeliveries = 10000

// deliveryTracker counts deliveries. Quorum queues carry the broker's own
// count in x-delivery-count; classic queues only flag Redelivered, so this
// consumer counts the redeliveries it sees by message-id. That count is
// local: with several consumers on the queue the limit may take up to
// MaxDeliveries per consumer to trigger.
type deliveryTracker struct {
	mu    sync.Mutex
	nacks map[string]int // message-id -> deliveries seen here
}

func newDeliveryTracker() *deliveryTracker {
	return &deliveryTracker{nacks: make(map[string]int)}
}

func (t *deliveryTracker) count(d *amqp091.Delivery) int {
	if n, ok := deliveryCountHeader(d.Headers); ok {
		return n + 1 // x-delivery-count counts the previous deliveries
	}
	if !d.Redelivered {
		return 1
	}
	if t == nil || d.MessageId == "" {
		return 2
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return max(t.nacks[d.MessageId], 1) + 1
}

func (t *deliveryTracker) nacked(d *amqp091.Delivery, count int) {
	if t == nil || d.MessageId == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.nacks) >= maxTrackedDeliveries {
		clear(t.nacks)
	}
	t.nacks[d.MessageId] = count
}

func (t *deliveryTracker) settled(d *amqp091.Delivery) {
	if t == nil || d.MessageId == "" {
		return
	}
	t.mu.Lock()
	delete(t.nacks, d.MessageId)
	t.mu.Unlock()
}

func deliveryCountHeader(h amqp091.Table) (int, bool) {
	switch v := h["x-delivery-count"].(type) {
	case int64:
		return int(v), true
	case int32:
		return int(v), true
	case int:
		return v, true
	case int16:
		return int(v), true
	case uint8:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}

func (c *amqpConsumer) Close() error {
	ch, _ := c.current()
	return ch.Close()
}

// Pause cancels this consumer (client-initiated channel.flow is not supported by
// RabbitMQ); Resume lets Consume register it again.
func (c *amqpConsumer) Pause() error {
	c.gate.Pause()
	c.canceled.Store(true)
	ch, _ := c.current()
	return ch.Cancel(c.tag, false)
}

func (c *amqpConsumer) Resume() error {
	c.gate.Resume()
	return nil
}

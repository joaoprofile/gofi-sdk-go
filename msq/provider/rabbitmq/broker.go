package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/msq/worker"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/rabbitmq/amqp091-go"
)

// amqpChannel abstracts *amqp091.Channel so that it can be mocked in tests.
type amqpChannel interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp091.Publishing) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp091.Table) (<-chan amqp091.Delivery, error)
	Qos(prefetchCount, prefetchSize int, global bool) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp091.Table) (amqp091.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp091.Table) error
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp091.Table) error
	Flow(active bool) error
	Close() error
}

// chanOpener abstracts *Conn so that the Broker can work without a real AMQP connection.
type chanOpener interface {
	channel() (amqpChannel, error)
}

// Broker implements port.Broker for RabbitMQ.
type Broker struct {
	conn     chanOpener
	exchange string
}

// New creates a Broker bound to the given connection and exchange.
func New(conn *Conn, exchange string) *Broker {
	return &Broker{conn: conn, exchange: exchange}
}

func (b *Broker) NewProducer() port.Producer {
	ch, err := b.conn.channel()
	if err != nil {
		logging.Error("rabbitmq: failed to open producer channel", slog.Any("error", err))
		return nil
	}
	return &amqpProducer{channel: ch, exchange: b.exchange}
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}

	ch, err := b.conn.channel()
	if err != nil {
		logging.Error("rabbitmq: failed to open consumer channel", slog.Any("error", err))
		return nil
	}

	if err := ch.Qos(concurrency, 0, false); err != nil {
		ch.Close()
		logging.Error("rabbitmq: QoS failed", slog.Any("error", err))
		return nil
	}

	routingKey := cfg.RoutingKey
	if routingKey == "" {
		routingKey = cfg.Topic
	}

	q, err := ch.QueueDeclare(cfg.Topic, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		logging.Error("rabbitmq: queue declare failed", slog.String("queue", cfg.Topic), slog.Any("error", err))
		return nil
	}

	if err := ch.QueueBind(cfg.Topic, routingKey, b.exchange, false, nil); err != nil {
		ch.Close()
		logging.Error("rabbitmq: queue bind failed", slog.String("queue", cfg.Topic), slog.Any("error", err))
		return nil
	}

	return &amqpConsumer{
		channel:     ch,
		queue:       q,
		cfg:         cfg,
		concurrency: concurrency,
	}
}

// Producer

type amqpProducer struct {
	channel  amqpChannel
	exchange string
}

func (p *amqpProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("rabbitmq producer: marshal failed: %w", err)
	}
	routingKey := msg.Key
	if routingKey == "" {
		routingKey = msg.Topic
	}
	return p.channel.PublishWithContext(ctx, p.exchange, routingKey, false, false,
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			Body:         body,
		},
	)
}

func (p *amqpProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	for _, msg := range msgs {
		if err := p.SendMessage(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (p *amqpProducer) Close() error { return p.channel.Close() }

// Consumer

type amqpConsumer struct {
	channel     amqpChannel
	queue       amqp091.Queue
	cfg         types.ConsumeConfig
	concurrency int
	paused      atomic.Bool
}

func (c *amqpConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	deliveries, err := c.channel.Consume(c.queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq consumer: register failed: %w", err)
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()

	go func() {
		<-ctx.Done()
		c.channel.Close()
	}()

	for d := range deliveries {
		d := d
		pool.Enqueue(func() { c.handle(ctx, &d, handler) })
	}
	return nil
}

func (c *amqpConsumer) handle(ctx context.Context, d *amqp091.Delivery, handler port.MessageHandler) {
	var msg types.Message
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		logging.Error("rabbitmq consumer: unmarshal failed, discarding", slog.Any("error", err))
		d.Nack(false, false)
		return
	}

	result, err := handler.Handle(ctx, &msg)

	switch result {
	case types.Ack:
		d.Ack(false)
	case types.Nack:
		d.Nack(false, err != nil)
	case types.Ignore:
		// no ack
	default:
		d.Ack(false)
	}
}

func (c *amqpConsumer) Close() error  { return c.channel.Close() }
func (c *amqpConsumer) Pause() error  { c.paused.Store(true); return c.channel.Flow(false) }
func (c *amqpConsumer) Resume() error { c.paused.Store(false); return c.channel.Flow(true) }

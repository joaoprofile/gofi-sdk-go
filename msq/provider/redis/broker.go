package redis

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
	goredis "github.com/redis/go-redis/v9"
)

// Config configures the Redis Pub/Sub broker.
type Config struct {
	// Standalone mode.
	Addr     string
	Password string
	DB       int

	// Cluster mode (takes precedence over Addr when set).
	ClusterAddrs []string

	// TLS
	TLSEnabled bool

	// Connection pool.
	PoolSize     int
	MinIdleConns int
}

// Broker implements port.Broker using Redis Pub/Sub.
type Broker struct {
	client goredis.UniversalClient
}

// New creates a Broker from configuration.
func New(cfg Config) *Broker {
	var client goredis.UniversalClient
	if len(cfg.ClusterAddrs) > 0 {
		client = goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        cfg.ClusterAddrs,
			Password:     cfg.Password,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
		})
	} else {
		client = goredis.NewClient(&goredis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
		})
	}
	return &Broker{client: client}
}

// NewWithClient creates a Broker reusing an existing Redis client.
// Useful for sharing the connection pool with other packages (e.g. cache, session).
func NewWithClient(client goredis.UniversalClient) *Broker {
	return &Broker{client: client}
}

// NewProducer returns a Redis Pub/Sub producer.
func (b *Broker) NewProducer() (port.Producer, error) {
	return &producer{client: b.client}, nil
}

// NewConsumer returns a Redis Pub/Sub consumer subscribed to cfg.Topic.
func (b *Broker) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	return &consumer{
		client:      b.client,
		cfg:         cfg,
		concurrency: concurrency,
	}
}

// Producer

type producer struct {
	client goredis.UniversalClient
}

// SendMessage publishes a message to a Redis channel identified by msg.Topic.
// Redis Pub/Sub is fire-and-forget; delivery is not guaranteed if no subscriber is active.
func (p *producer) SendMessage(ctx context.Context, msg *types.Message) error {
	if msg.Topic == "" {
		return fmt.Errorf("redis producer: msg.Topic (channel) must be set")
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("redis producer: marshal failed: %w", err)
	}
	if err := p.client.Publish(ctx, msg.Topic, payload).Err(); err != nil {
		return fmt.Errorf("redis producer: publish to %q failed: %w", msg.Topic, err)
	}
	return nil
}

// SendMessagesBatch publishes multiple messages, each to its own topic.
// Uses pipelining to minimise round-trips when all messages share the same topic.
func (p *producer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	pipe := p.client.Pipeline()
	for _, msg := range msgs {
		if msg.Topic == "" {
			return fmt.Errorf("redis producer batch: msg.Topic must be set for all messages")
		}
		payload, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("redis producer batch: marshal failed: %w", err)
		}
		pipe.Publish(ctx, msg.Topic, payload)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis producer batch: pipeline exec failed: %w", err)
	}
	return nil
}

func (p *producer) Close() error { return nil }

// Consumer

type consumer struct {
	client      goredis.UniversalClient
	cfg         types.ConsumeConfig
	concurrency int
	paused      atomic.Bool
}

// Consume subscribes to cfg.Topic and dispatches received messages to handler.
// Blocks until ctx is cancelled. Each message is dispatched to a worker pool
// to allow concurrent processing up to cfg.Concurrency goroutines.
//
// Pub/Sub ack semantics:
//   - Ack or nil error from handler: message is considered processed.
//   - Nack or non-nil error from handler: logged; no requeue possible with Pub/Sub.
//   - Ignore: message silently skipped.
func (c *consumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	sub := c.client.Subscribe(ctx, c.cfg.Topic)
	defer sub.Close()

	pool := worker.New(c.concurrency)
	defer pool.Close()

	msgCh := sub.Channel()

	logging.Info("redis consumer: subscribed", slog.String("channel", c.cfg.Topic))

	for {
		select {
		case <-ctx.Done():
			logging.Info("redis consumer: context cancelled", slog.String("channel", c.cfg.Topic))
			return nil

		case redisMsg, ok := <-msgCh:
			if !ok {
				logging.Info("redis consumer: channel closed", slog.String("channel", c.cfg.Topic))
				return nil
			}
			if c.paused.Load() {
				continue
			}

			payload := redisMsg.Payload
			pool.Enqueue(func() {
				c.handle(ctx, payload, handler)
			})
		}
	}
}

func (c *consumer) handle(ctx context.Context, payload string, handler port.MessageHandler) {
	var msg types.Message
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		// Payload is not a wrapped Message — treat the raw string as the value.
		msg = types.Message{Value: []byte(payload)}
	}

	result, err := handler.Handle(ctx, &msg)

	switch result {
	case types.Ack:
		// No explicit ack needed in Pub/Sub.
	case types.Nack:
		logging.Error("redis consumer: handler nacked message (no requeue in Pub/Sub)",
			slog.String("channel", c.cfg.Topic),
			slog.Any("error", err))
	case types.Ignore:
		// Caller explicitly skips this message.
	}
}

func (c *consumer) Close() error { return nil }

func (c *consumer) Pause() error {
	c.paused.Store(true)
	return nil
}

func (c *consumer) Resume() error {
	c.paused.Store(false)
	return nil
}

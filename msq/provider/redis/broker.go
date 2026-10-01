package redis

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/joaoprofile/gofi-sdk-go/msq/worker"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	goredis "github.com/redis/go-redis/v9"
)

// Mode selects the Redis messaging primitive.
type Mode string

const (
	// ModePubSub uses Pub/Sub: at-most-once, subscribers must be connected.
	ModePubSub Mode = "pubsub"
	// ModeStreams uses Streams with consumer groups: at-least-once,
	// messages wait for consumers and Nacks are redelivered.
	ModeStreams Mode = "streams"
)

// Config configures the Redis broker. Printing or logging it redacts the
// password.
type Config struct {
	// Mode defaults to ModePubSub.
	Mode Mode
	// StreamMaxLen caps each stream approximately (XADD MAXLEN ~); 0 keeps all.
	StreamMaxLen int64
	// ClaimIdle is how long an unacked stream message waits before it is
	// redelivered (default 1m); ConsumeConfig.VisibilityTimeout overrides it.
	ClaimIdle time.Duration

	// Standalone mode.
	Addr     string
	Password string
	DB       int

	// Cluster mode (takes precedence over Addr when set).
	ClusterAddrs []string

	// TLS
	TLSEnabled bool
	// TLS is the client TLS configuration (private CA, mTLS, server name);
	// setting it enables TLS. Versions below TLS 1.2 are raised to it.
	TLS *tls.Config `json:"-"`

	// Connection pool.
	PoolSize     int
	MinIdleConns int
}

// Broker implements port.Broker using Redis Pub/Sub or Streams.
type Broker struct {
	client goredis.UniversalClient
	cfg    Config
}

// New creates a Broker from configuration.
func New(cfg Config) *Broker {
	var client goredis.UniversalClient
	if len(cfg.ClusterAddrs) > 0 {
		client = goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        cfg.ClusterAddrs,
			Password:     cfg.Password,
			TLSConfig:    tlsConfig(cfg),
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
		})
	} else {
		client = goredis.NewClient(&goredis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			TLSConfig:    tlsConfig(cfg),
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
		})
	}
	return &Broker{client: client, cfg: cfg}
}

// NewWithClient creates a Broker reusing an existing Redis client, e.g. the
// cache pool. Only the Mode, StreamMaxLen and ClaimIdle fields of cfg apply.
func NewWithClient(client goredis.UniversalClient, cfg ...Config) *Broker {
	b := &Broker{client: client}
	if len(cfg) > 0 {
		b.cfg = cfg[0]
	}
	return b
}

// NewProducer returns a producer for the configured mode.
func (b *Broker) NewProducer() (port.Producer, error) {
	if b.cfg.Mode == ModeStreams {
		return &streamProducer{client: b.client, maxLen: b.cfg.StreamMaxLen}, nil
	}
	return &producer{client: b.client}, nil
}

// NewConsumer returns a consumer of cfg.Topic for the configured mode.
func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	switch b.cfg.Mode {
	case "", ModePubSub:
	case ModeStreams:
		return newStreamConsumer(b.client, cfg, concurrency, b.cfg.ClaimIdle), nil
	default:
		return nil, fmt.Errorf("redis: unknown mode %q", b.cfg.Mode)
	}
	return &consumer{
		client:      b.client,
		cfg:         cfg,
		concurrency: concurrency,
	}, nil
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

			payload, channel := redisMsg.Payload, redisMsg.Channel
			pool.Enqueue(func() {
				c.handle(ctx, channel, payload, handler)
			})
		}
	}
}

func (c *consumer) handle(ctx context.Context, channel, payload string, handler port.MessageHandler) {
	msg := types.DecodeEnvelope([]byte(payload))
	msg.Topic = cmp.Or(channel, c.cfg.Topic)
	msg.DeliveryCount = 1 // Pub/Sub never redelivers
	if msg.Id == uuid.Nil {
		msg.Id = uuid.New() // Pub/Sub has no message id
	}

	result, err := handler.Handle(ctx, &msg)

	switch result {
	case types.Ack:
		// No explicit ack needed in Pub/Sub.
	case types.Nack, types.Reject:
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

// tlsConfig returns a TLS 1.2+ config when enabled; without a custom
// ServerName the name is taken from each dial address.
func tlsConfig(cfg Config) *tls.Config {
	if cfg.TLS != nil {
		t := cfg.TLS.Clone()
		t.MinVersion = max(t.MinVersion, tls.VersionTLS12)
		return t
	}
	if !cfg.TLSEnabled {
		return nil
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

// String implements fmt.Stringer with the password redacted.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with the password redacted.
func (c Config) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with the password redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the configuration with the password redacted.
func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

type plainConfig Config

func (c Config) redacted() plainConfig {
	if c.Password != "" {
		c.Password = "[REDACTED]"
	}
	c.TLS = nil // holds private keys
	return plainConfig(c)
}

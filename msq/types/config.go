package types

import "time"

const (
	DefaultConcurrency  = 20
	DefaultPollInterval = 10 * time.Second
	// DefaultVisibilityTimeout is the lease a received message gets on
	// SQS, OCI Queue and NATS when ConsumeConfig.VisibilityTimeout is zero.
	DefaultVisibilityTimeout = 30 * time.Second
	// DefaultMaxDeliveries bounds how often the broker hands the same message
	// to a handler that keeps nacking it (poison message).
	DefaultMaxDeliveries = 10
	// DefaultHandlerTimeout bounds one handler attempt when
	// ConsumeConfig.HandlerTimeout is zero. The lease is renewed meanwhile, so
	// it only has to outlast the slowest legitimate handler; its job is to
	// keep a stuck handler from blocking shutdown forever.
	DefaultHandlerTimeout = 5 * time.Minute
)

// OffsetReset controls where a consumer group starts reading when it has no
// committed offset (its first run). It is ignored once the group has committed
// offsets, and ignored by brokers without the concept (RabbitMQ, Redis).
type OffsetReset string

const (
	// OffsetResetDefault defers to the provider default (Kafka: latest).
	OffsetResetDefault OffsetReset = ""
	// OffsetResetEarliest starts from the beginning of the topic on first run —
	// use for command/event topics that must not be lost.
	OffsetResetEarliest OffsetReset = "earliest"
	// OffsetResetLatest starts from the end of the topic on first run — use for
	// live-tail consumers that should ignore backlog.
	OffsetResetLatest OffsetReset = "latest"
)

// ConsumeConfig holds all configuration for a consumer, regardless of broker.
// Unused fields are silently ignored by providers that do not support them.
type ConsumeConfig struct {
	GroupID         string        // consumer group identifier (Kafka, SQS)
	Topic           string        // topic / queue name / Redis channel
	RoutingKey      string        // AMQP routing key (RabbitMQ only)
	QueueID         string        // provider-assigned queue ID (OCI only)
	Concurrency     int           // number of parallel goroutines
	PollInterval    time.Duration // polling interval for pull-based brokers
	MaxRetries      int           // in-process retries after a Nack (0 = none)
	RetryBackoff    time.Duration // first retry wait, doubled with jitter up to 30s (default 1s)
	DeadLetterTopic string        // receives a copy once retries are exhausted; the original is then acked
	HandlerTimeout  time.Duration // per-attempt handler deadline (0 = DefaultHandlerTimeout, negative = none)
	InitialOffset   OffsetReset   // where to start when the group has no committed offset (Kafka)

	// VisibilityTimeout is how long a received message stays leased to this
	// consumer before the broker redelivers it (SQS and OCI visibility, NATS
	// AckWait, Redis Streams claim idle). The lease is renewed every third of
	// it while the message waits for and runs in its handler, so it bounds
	// crash recovery, not handler time. 0 = DefaultVisibilityTimeout (Redis
	// Streams: Config.ClaimIdle). NATS applies it when it creates the durable.
	VisibilityTimeout time.Duration

	// MaxDeliveries is how many times the broker may deliver a message that
	// keeps failing. On the last one, a message still nacked goes to
	// DeadLetterTopic when set; otherwise it is rejected (dropped, or routed
	// by the broker's own dead-letter setup such as a RabbitMQ DLX) with an
	// EventMessageRejected. Each delivery runs up to 1+MaxRetries attempts.
	// 0 = DefaultMaxDeliveries with a DeadLetterTopic and unlimited without
	// one, so a message is never dropped unless asked; negative = unlimited
	// (defer to the broker, e.g. an SQS redrive policy).
	MaxDeliveries int
}

// DeliveryLimit returns the effective MaxDeliveries; 0 means unlimited.
func (c ConsumeConfig) DeliveryLimit() int {
	switch {
	case c.MaxDeliveries < 0:
		return 0
	case c.MaxDeliveries == 0 && c.DeadLetterTopic == "":
		// Mission-critical default: without a DLQ the message keeps coming back.
		return 0
	case c.MaxDeliveries == 0:
		return DefaultMaxDeliveries
	default:
		return c.MaxDeliveries
	}
}

// DeliveryLimitReached reports whether a message delivered count times has
// used its last delivery. An unknown count (0) never reaches the limit.
func (c ConsumeConfig) DeliveryLimitReached(count int) bool {
	limit := c.DeliveryLimit()
	return limit > 0 && count >= limit
}

// EffectiveHandlerTimeout returns the handler deadline; 0 means none.
func (c ConsumeConfig) EffectiveHandlerTimeout() time.Duration {
	switch {
	case c.HandlerTimeout < 0:
		return 0
	case c.HandlerTimeout == 0:
		return DefaultHandlerTimeout
	default:
		return c.HandlerTimeout
	}
}

// DefaultConsumeConfig returns a ConsumeConfig with sensible defaults.
func DefaultConsumeConfig(topic string) ConsumeConfig {
	return ConsumeConfig{
		Topic:        topic,
		Concurrency:  DefaultConcurrency,
		PollInterval: DefaultPollInterval,
	}
}

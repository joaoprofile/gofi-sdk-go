package types

import "time"

const (
	DefaultConcurrency  = 20
	DefaultPollInterval = 10 * time.Second
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
	AutoCommit      bool          // deprecated, no-op: Kafka offsets always commit via Sarama's auto-commit
	PollInterval    time.Duration // polling interval for pull-based brokers
	MaxRetries      int           // max handler retries before dead-lettering
	RetryBackoff    time.Duration // wait between retries
	DeadLetterTopic string        // topic for messages that exhausted retries
	InitialOffset   OffsetReset   // where to start when the group has no committed offset (Kafka)
}

// DefaultConsumeConfig returns a ConsumeConfig with sensible defaults.
func DefaultConsumeConfig(topic string) ConsumeConfig {
	return ConsumeConfig{
		Topic:        topic,
		Concurrency:  DefaultConcurrency,
		PollInterval: DefaultPollInterval,
	}
}

// QueueAttributes identifies a queue or topic by broker-specific coordinates.
// Kept for compatibility with pkg/queue. New code should use ConsumeConfig directly.
type QueueAttributes struct {
	QueueName  string
	QueueID    string
	RoutingKey string
}

// ToConsumeConfig converts QueueAttributes to a ConsumeConfig with defaults.
func (q QueueAttributes) ToConsumeConfig() ConsumeConfig {
	return ConsumeConfig{
		Topic:       q.QueueName,
		RoutingKey:  q.RoutingKey,
		QueueID:     q.QueueID,
		Concurrency: DefaultConcurrency,
	}
}

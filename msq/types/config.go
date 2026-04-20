package types

import "time"

const (
	DefaultConcurrency  = 20
	DefaultPollInterval = 10 * time.Second
)

// ConsumeConfig holds all configuration for a consumer, regardless of broker.
// Unused fields are silently ignored by providers that do not support them.
type ConsumeConfig struct {
	GroupID         string        // consumer group identifier (Kafka, SQS)
	Topic           string        // topic / queue name / Redis channel
	RoutingKey      string        // AMQP routing key (RabbitMQ only)
	QueueID         string        // provider-assigned queue ID (OCI only)
	Concurrency     int           // number of parallel goroutines
	AutoCommit      bool          // auto-commit offsets (Kafka)
	PollInterval    time.Duration // polling interval for pull-based brokers
	MaxRetries      int           // max handler retries before dead-lettering
	RetryBackoff    time.Duration // wait between retries
	DeadLetterTopic string        // topic for messages that exhausted retries
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

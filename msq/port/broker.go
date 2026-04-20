package port

import (
	"context"

	"github.com/joaoprofile/gofi/msq/types"
)

// Broker is the central port that every messaging provider must implement.
// It is the only interface callers need to hold in order to create producers and consumers.
type Broker interface {
	// NewProducer returns a ready-to-use Producer.
	// Callers must call Producer.Close() when done.
	NewProducer() Producer

	// NewConsumer returns a Consumer configured for the given ConsumeConfig.
	// Callers must call Consumer.Close() when done.
	NewConsumer(cfg types.ConsumeConfig) Consumer
}

// BrokerSetup is implemented by brokers that require infrastructure setup before use
// (e.g. declaring a RabbitMQ exchange, or creating a Kafka topic).
// Call Setup once during application bootstrap.
type BrokerSetup interface {
	Setup(ctx context.Context) error
}

// BrokerFactory creates a Broker from configuration.
// Each provider implements this to participate in the typed factory registry.
type BrokerFactory interface {
	Build(ctx context.Context) (Broker, error)
}

// Connection represents a low-level broker connection.
// Kept for compatibility with existing connection lifecycle management.
type Connection interface {
	IsConnected() bool
	Close() error
}

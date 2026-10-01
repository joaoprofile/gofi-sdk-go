package port

import (
	"context"

	"github.com/joaoprofile/gofi-sdk-go/msq/types"
)

// Broker is the central port that every messaging provider must implement.
// It is the only interface callers need to hold in order to create producers and consumers.
type Broker interface {
	// NewProducer returns a ready-to-use Producer.
	// It returns a non-nil error when the producer cannot be created
	// (e.g. the broker is unreachable); callers must not ignore it.
	// Callers must call Producer.Close() when done.
	NewProducer() (Producer, error)

	// NewConsumer returns a Consumer configured for the given ConsumeConfig.
	// Callers must call Consumer.Close() when done.
	NewConsumer(cfg types.ConsumeConfig) (Consumer, error)
}

// BrokerSetup is implemented by brokers that require infrastructure setup before use
// (e.g. declaring a RabbitMQ exchange, or creating a Kafka topic).
// Call Setup once during application bootstrap.
type BrokerSetup interface {
	Setup(ctx context.Context) error
}

package port

import (
	"context"

	"github.com/joaoprofile/gofi/msq/types"
)

// Producer sends messages to a broker topic or queue.
// A single Producer instance should not be shared across goroutines without
// external synchronization; prefer creating one Producer per goroutine or use
// the batch API for bulk operations.
type Producer interface {
	// SendMessage sends a single message. msg.Topic must be set.
	SendMessage(ctx context.Context, msg *types.Message) error

	// SendMessagesBatch sends multiple messages.
	// Providers that support native batching (SQS, Kafka) will use it;
	// others will iterate sequentially.
	SendMessagesBatch(ctx context.Context, msgs []*types.Message) error

	// Close releases all resources held by this Producer.
	Close() error
}

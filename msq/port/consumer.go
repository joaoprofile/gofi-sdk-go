package port

import (
	"context"
)

// Consumer receives and processes messages from a broker topic or queue.
type Consumer interface {
	// Consume blocks and delivers messages to handler until ctx is cancelled.
	// Returns a non-nil error only for unrecoverable failures; cancellation returns nil.
	Consume(ctx context.Context, handler MessageHandler) error

	// Close stops message delivery and releases all resources.
	Close() error

	// Pause temporarily halts message delivery without closing the consumer.
	// Useful for backpressure and rate-limiting scenarios.
	Pause() error

	// Resume restores message delivery after a Pause.
	Resume() error
}

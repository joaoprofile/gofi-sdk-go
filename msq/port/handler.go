package port

import (
	"context"

	"github.com/joaoprofile/gofi/msq/types"
)

// MessageHandler is the interface for processing broker messages.
//
// Return semantics:
//   - (Ack, nil)    — message processed successfully; commit offset or delete from queue
//   - (Nack, err)   — processing failed; requeue if the broker supports it
//   - (Ignore, nil) — skip explicit acknowledgment; let broker visibility timeout decide
type MessageHandler interface {
	Handle(ctx context.Context, msg *types.Message) (types.Result, error)
}

// MessageHandlerFunc is a func type that implements MessageHandler.
// Allows existing handler functions to satisfy the interface with zero wrapping.
//
// Example:
//
//	manager.Register(cfg, msq.MessageHandlerFunc(myHandlerFunc))
type MessageHandlerFunc func(ctx context.Context, msg *types.Message) (types.Result, error)

func (f MessageHandlerFunc) Handle(ctx context.Context, msg *types.Message) (types.Result, error) {
	return f(ctx, msg)
}

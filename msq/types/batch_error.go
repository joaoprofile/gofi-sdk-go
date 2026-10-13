package types

import (
	"fmt"
	"slices"
)

// BatchSendError reports a partial batch send: Failed holds the indexes, in the
// slice given to SendMessagesBatch, of the messages the broker did not accept.
// Every other message was delivered. Providers that cannot tell which messages
// failed return a plain error instead, and callers must then treat the whole
// batch as failed.
type BatchSendError struct {
	Failed []int
	Err    error
}

func (e *BatchSendError) Error() string {
	return fmt.Sprintf("batch send: %d message(s) failed: %v", len(e.Failed), e.Err)
}

func (e *BatchSendError) Unwrap() error { return e.Err }

// FailedIndex reports whether the message at index i was not delivered.
func (e *BatchSendError) FailedIndex(i int) bool { return slices.Contains(e.Failed, i) }

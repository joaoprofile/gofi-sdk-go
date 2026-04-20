package types

// Result signals the broker how to handle a processed message.
type Result int

const (
	Ack    Result = iota // message processed: commit offset / delete from queue
	Nack                 // processing failed: requeue / retry
	Ignore               // skip explicit ack; let broker decide (e.g. visibility timeout)
)

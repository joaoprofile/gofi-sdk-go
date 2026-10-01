package types

// Result signals the broker how to handle a processed message.
type Result int

const (
	Ack    Result = iota // message processed: commit offset / delete from queue
	Nack                 // processing failed: requeue / retry
	Ignore               // discarded on purpose: removed from the queue, no requeue
	// Reject gives up on a message that cannot be processed: it is removed
	// without requeue and, where the broker has its own dead-letter route
	// (RabbitMQ DLX, NATS terminate advisory), handed to it. The pipeline
	// returns it once MaxDeliveries is reached without a DeadLetterTopic.
	Reject
)

package types

import "time"

// BrokerEvent represents an observable event within the messaging lifecycle.
// Passed to the OnEvent callback configured in msq.Config.
type BrokerEvent struct {
	Type      BrokerEventType
	Topic     string
	MessageID string
	Error     error
	Timestamp time.Time
}

// BrokerEventType identifies the kind of messaging event.
type BrokerEventType string

const (
	EventMessageSent         BrokerEventType = "message_sent"
	EventMessageReceived     BrokerEventType = "message_received"
	EventMessageAcked        BrokerEventType = "message_acked"
	EventMessageNacked       BrokerEventType = "message_nacked"
	EventConsumerStarted     BrokerEventType = "consumer_started"
	EventConsumerStopped     BrokerEventType = "consumer_stopped"
	EventProducerError       BrokerEventType = "producer_error"
	EventConsumerError       BrokerEventType = "consumer_error"
	EventMessageDeadLettered BrokerEventType = "message_dead_lettered"
	// EventMessageRejected: the delivery limit was reached with no
	// DeadLetterTopic, so the message was rejected (dropped or broker DLX).
	EventMessageRejected BrokerEventType = "message_rejected"
	// EventConsumerRestarting: a consumer failed and is restarted with backoff.
	EventConsumerRestarting BrokerEventType = "consumer_restarting"
)

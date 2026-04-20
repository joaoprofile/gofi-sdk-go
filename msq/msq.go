package msq

import (
	"context"

	"github.com/joaoprofile/gofi/msq/core"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
)

// ConsumerManager orchestrates multiple consumers against a single broker.
// Obtain via NewConsumerManager or BrokerService.NewConsumerManager().
type ConsumerManager = core.ConsumerManager

// NewConsumerManager creates a ConsumerManager backed by the given Broker.
// Register consumers with Register, then call Start or Dispatcher.
func NewConsumerManager(broker port.Broker) *core.ConsumerManager {
	return core.NewConsumerManager(broker)
}

// Constructor configures an msq provider for registration with the GOFI builder.
// Pass to Builder.AddMessaging when wiring messaging into a GOFI service.
type Constructor struct {
	// Factory builds the Broker on demand.
	Factory port.BrokerFactory
}

//  Type re-exports
// Callers import only this package for the common case.

type (
	// Message is the universal broker envelope.
	Message = types.Message

	// ConsumeConfig configures a consumer, regardless of broker.
	ConsumeConfig = types.ConsumeConfig

	// QueueAttributes identifies a queue by broker-specific coordinates.
	// Prefer ConsumeConfig in new code.
	QueueAttributes = types.QueueAttributes

	// Result signals the broker how to handle a processed message.
	Result = types.Result

	// BrokerEvent carries observability payloads emitted during broker lifecycle.
	BrokerEvent = types.BrokerEvent

	// Producer sends messages to a broker.
	Producer = port.Producer

	// Consumer receives and processes messages from a broker.
	Consumer = port.Consumer

	// MessageHandler processes a single broker message.
	MessageHandler = port.MessageHandler

	// MessageHandlerFunc adapts a plain function to MessageHandler.
	MessageHandlerFunc = port.MessageHandlerFunc

	// Broker is the central port every messaging provider must implement.
	Broker = port.Broker

	// MessagingBroker is an alias for Broker kept for backward compatibility.
	MessagingBroker = port.Broker

	// Messaging is a backward-compatible alias for Broker.
	// Prefer Broker in new code.
	Messaging = port.Broker

	// BrokerSetup is implemented by brokers that require infrastructure setup
	// before producing or consuming (e.g. RabbitMQ exchange declaration).
	BrokerSetup = port.BrokerSetup

	// BrokerFactory creates a Broker from configuration.
	BrokerFactory = port.BrokerFactory

	// Connection represents a low-level broker connection.
	Connection = port.Connection
)

// Result constants re-exported at package level.
const (
	Ack    = types.Ack
	Nack   = types.Nack
	Ignore = types.Ignore
)

// Consumer concurrency / polling defaults re-exported at package level.
const (
	DefaultConcurrency  = types.DefaultConcurrency
	DefaultPollInterval = types.DefaultPollInterval
)

//  Message constructo

// NewMessage creates a Message with the payload serialized as JSON.
// Set Topic via WithTopic or directly before sending.
func NewMessage(value interface{}) *Message {
	return types.NewMessage(value)
}

// NewMessageWithTopic creates a Message with topic and payload already set.
func NewMessageWithTopic(topic string, value interface{}) *Message {
	return types.NewMessageWithTopic(topic, value)
}

// UnpackMessage decodes the message Value into T using type inference.
//
//	order, err := msq.UnpackMessage[Order](msg)
func UnpackMessage[T any](message *Message) (*T, error) {
	return types.UnpackMessage[T](message)
}

// DefaultConsumeConfig returns a ConsumeConfig with sensible defaults for the
// given topic.
func DefaultConsumeConfig(topic string) ConsumeConfig {
	return types.DefaultConsumeConfig(topic)
}

//  Broker type

// BrokerType identifies a messaging provider so that AddMessaging can build
// the broker automatically from environment variables.
// Values are intentionally lowercase strings to match MESSAGING_PROVIDER env values.
type BrokerType string

const (
	BrokerKafka    BrokerType = "kafka"
	BrokerRabbitMQ BrokerType = "rabbitmq"
	BrokerSQS      BrokerType = "sqs"
	BrokerOCI      BrokerType = "oci"
	BrokerRedis    BrokerType = "redis"
)

//  Service config

// Config configures a messaging provider for use with the GOFI builder.
//
// There are three ways to supply the broker, in order of precedence:
//
//  1. Broker — explicit port.Broker instance (full control / custom config).
//  2. BrokerType — builds the broker from environment variables automatically.
//  3. Neither — AddMessaging reads MESSAGING_PROVIDER from env and auto-selects.
type Config struct {
	// BrokerType instructs AddMessaging to build the broker from env vars.
	// Ignored when Broker is set explicitly.
	BrokerType BrokerType

	// Exchange is the AMQP exchange name used by the RabbitMQ provider.
	// Ignored by all other providers. Defaults to "" (AMQP default exchange).
	Exchange string

	// Broker is an explicit provider instance.
	// Use when you need configuration beyond what environment variables provide.
	Broker port.Broker

	// OnEvent is called for every broker lifecycle event. Optional.
	OnEvent func(ctx context.Context, event types.BrokerEvent)
}

//  Constructor

// New builds a BrokerService. Broker must be non-nil.
func New(cfg Config) (*core.BrokerService, error) {
	if cfg.Broker == nil {
		return nil, core.ErrBrokerRequired
	}
	return core.NewService(core.ServiceConfig{
		Broker:  cfg.Broker,
		OnEvent: cfg.OnEvent,
	}), nil
}

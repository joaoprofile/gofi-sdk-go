package msq

import (
	"context"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/core"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
)

// ConsumerManager orchestrates multiple consumers against a single broker.
// Obtain via NewConsumerManager or BrokerService.NewConsumerManager().
type ConsumerManager = core.ConsumerManager

// NewConsumerManager creates a ConsumerManager backed by the given Broker.
// Register consumers with Register, then call Start or Dispatcher.
func NewConsumerManager(broker port.Broker) *core.ConsumerManager {
	return core.NewConsumerManager(broker)
}

//  Type re-exports
// Callers import only this package for the common case.

type (
	// Message is the universal broker envelope.
	Message = types.Message

	// ConsumeConfig configures a consumer, regardless of broker.
	ConsumeConfig = types.ConsumeConfig

	// OffsetReset controls where a consumer group starts with no committed offset.
	OffsetReset = types.OffsetReset

	// Encoding selects the wire format of providers that support more than one.
	Encoding = types.Encoding

	// Result signals the broker how to handle a processed message.
	Result = types.Result

	// BrokerEvent carries observability payloads emitted during broker lifecycle.
	BrokerEvent = types.BrokerEvent

	// BrokerEventType identifies the kind of BrokerEvent.
	BrokerEventType = types.BrokerEventType

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

	// BrokerSetup is implemented by brokers that require infrastructure setup
	// before producing or consuming (e.g. RabbitMQ exchange declaration).
	BrokerSetup = port.BrokerSetup
)

// Event types re-exported at package level.
const (
	EventMessageSent         = types.EventMessageSent
	EventMessageReceived     = types.EventMessageReceived
	EventMessageAcked        = types.EventMessageAcked
	EventMessageNacked       = types.EventMessageNacked
	EventMessageDeadLettered = types.EventMessageDeadLettered
	EventConsumerStarted     = types.EventConsumerStarted
	EventConsumerStopped     = types.EventConsumerStopped
	EventProducerError       = types.EventProducerError
	EventConsumerError       = types.EventConsumerError
	EventMessageRejected     = types.EventMessageRejected
	EventConsumerRestarting  = types.EventConsumerRestarting
)

// Dead-letter headers set on the copy published to ConsumeConfig.DeadLetterTopic.
const (
	HeaderDLQOriginalTopic = core.HeaderDLQOriginalTopic
	HeaderDLQError         = core.HeaderDLQError
	HeaderDLQAttempts      = core.HeaderDLQAttempts
	HeaderDLQDeliveries    = core.HeaderDLQDeliveries
)

// Wire formats re-exported at package level.
const (
	EncodingEnvelope    = types.EncodingEnvelope
	EncodingCloudEvents = types.EncodingCloudEvents
)

// Result constants re-exported at package level.
const (
	Ack    = types.Ack
	Nack   = types.Nack
	Ignore = types.Ignore
	Reject = types.Reject
)

// Consumer concurrency / polling / lease defaults re-exported at package level.
const (
	DefaultConcurrency       = types.DefaultConcurrency
	DefaultPollInterval      = types.DefaultPollInterval
	DefaultVisibilityTimeout = types.DefaultVisibilityTimeout
	DefaultMaxDeliveries     = types.DefaultMaxDeliveries
	DefaultHandlerTimeout    = types.DefaultHandlerTimeout
)

//  Message constructors

// NewMessage creates a Message with the payload serialized as JSON.
// Set Topic via WithTopic or directly before sending.
func NewMessage(value any) (*Message, error) {
	return types.NewMessage(value)
}

// NewMessageWithTopic creates a Message with topic and payload already set.
func NewMessageWithTopic(topic string, value any) (*Message, error) {
	return types.NewMessageWithTopic(topic, value)
}

// UnpackMessage decodes the message Value into T using type inference.
//
//	order, err := msq.UnpackMessage[Order](msg)
func UnpackMessage[T any](message *Message) (*T, error) {
	return types.UnpackMessage[T](message)
}

// OffsetReset values re-exported so callers import only this package.
const (
	OffsetResetDefault  = types.OffsetResetDefault
	OffsetResetEarliest = types.OffsetResetEarliest
	OffsetResetLatest   = types.OffsetResetLatest
)

// DefaultConsumeConfig returns a ConsumeConfig with sensible defaults for the
// given topic.
func DefaultConsumeConfig(topic string) ConsumeConfig {
	return types.DefaultConsumeConfig(topic)
}

//  Broker type

// BrokerType identifies a messaging provider so that AddMessaging can build
// the broker automatically from environment variables (see Register).
// Values are intentionally lowercase strings to match MESSAGING_PROVIDER env values.
type BrokerType string

const (
	BrokerKafka    BrokerType = "kafka"
	BrokerRabbitMQ BrokerType = "rabbitmq"
	BrokerSQS      BrokerType = "sqs"
	BrokerOCI      BrokerType = "oci"
	BrokerRedis    BrokerType = "redis"
	BrokerNATS     BrokerType = "nats"
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

	// System is the OpenTelemetry messaging.system attribute; defaults to the
	// semantic-convention value of BrokerType.
	System string

	// OnEvent is called for every message and consumer lifecycle event. It runs
	// on the hot path, so keep it cheap. Optional.
	OnEvent func(ctx context.Context, event types.BrokerEvent)
	// MaxDeliveries and HandlerTimeout are service-wide defaults for the
	// ConsumeConfig fields of the same name a consumer leaves at zero.
	MaxDeliveries  int
	HandlerTimeout time.Duration
}

//  Constructor

// New builds a BrokerService. Broker must be non-nil.
func New(cfg Config) (*core.BrokerService, error) {
	if cfg.Broker == nil {
		return nil, core.ErrBrokerRequired
	}
	system := cfg.System
	if system == "" {
		system = systems[cfg.BrokerType]
	}
	return core.NewService(core.ServiceConfig{
		Broker:  cfg.Broker,
		System:  system,
		OnEvent: cfg.OnEvent,
		Defaults: core.ConsumeDefaults{
			MaxDeliveries:  cfg.MaxDeliveries,
			HandlerTimeout: cfg.HandlerTimeout,
		},
	}), nil
}

// systems maps broker types to OpenTelemetry messaging.system values.
var systems = map[BrokerType]string{
	BrokerKafka:    "kafka",
	BrokerRabbitMQ: "rabbitmq",
	BrokerSQS:      "aws_sqs",
	BrokerOCI:      "oci_queue",
	BrokerRedis:    "redis",
	BrokerNATS:     "nats",
}

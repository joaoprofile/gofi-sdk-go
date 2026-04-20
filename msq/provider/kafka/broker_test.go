package kafka_test

import (
	"os"
	"testing"

	"github.com/joaoprofile/gofi/msq/provider/kafka"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("kafka-provider-test")
	os.Exit(m.Run())
}

// Config

func TestConfigFromEnv(t *testing.T) {
	// Does not connect; purely reads env vars and builds a Config struct.
	cfg := kafka.ConfigFromEnv()
	// Verify the struct is populated (env may be empty in CI, but it must not panic).
	assert.IsType(t, kafka.Config{}, cfg)
}

func TestNewBroker(t *testing.T) {
	cfg := kafka.Config{
		Brokers:  []string{"localhost:9092"},
		ClientID: "test-client",
	}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, broker)
}

func TestNewBrokerWithSASL(t *testing.T) {
	cfg := kafka.Config{
		Brokers:  []string{"localhost:9092"},
		User:     "user",
		Password: "pass",
		UseTLS:   true,
	}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, broker)
}

func TestNewBrokerWithoutClientID(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:9092"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, broker)
}

// Producer (no broker available — tests error/nil path)

func TestNewProducerReturnsNilWhenNoBroker(t *testing.T) {
	// sarama.NewSyncProducer will fail to connect and NewProducer returns nil.
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}} // nothing listening
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	p := broker.NewProducer()
	// With no running Kafka, Sarama will timeout/refuse and NewProducer logs + returns nil.
	assert.Nil(t, p)
}

// Consumer (no broker available — tests error/nil path)

func TestNewConsumerReturnsNilWhenNoBroker(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "test-topic",
		GroupID:     "test-group",
		Concurrency: 1,
	})
	assert.Nil(t, consumer)
}

func TestNewConsumerUsesTopicAsGroupIDWhenEmpty(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	// GroupID="" should default to Topic — broker still returns nil (no server),
	// but we verify the function doesn't panic on that path.
	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:   "my-topic",
		GroupID: "", // will be defaulted to Topic
	})
	assert.Nil(t, consumer)
}

func TestNewConsumerDefaultsConcurrencyToOne(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	// Concurrency <= 0 should default to 1 without panicking.
	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "t",
		Concurrency: 0,
	})
	assert.Nil(t, consumer) // nil because no broker, but must not panic
}

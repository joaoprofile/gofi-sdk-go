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

func TestNewProducerErrorsWhenNoBroker(t *testing.T) {
	// sarama.NewSyncProducer will fail to connect and NewProducer returns an error.
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}} // nothing listening
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	p, err := broker.NewProducer()
	// With no running Kafka, Sarama will timeout/refuse and NewProducer returns (nil, error).
	require.Error(t, err)
	assert.Nil(t, p)
}

// Consumer — NewConsumer is lazy: it neither connects nor creates a
// ConsumerGroup at construction time (that happens per worker in Consume), so it
// returns non-nil even with no broker available. The connection is only
// attempted once Consume runs.

func TestNewConsumerIsLazyAndReturnsConsumer(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "test-topic",
		GroupID:     "test-group",
		Concurrency: 1,
	})
	assert.NotNil(t, consumer)
}

func TestNewConsumerUsesTopicAsGroupIDWhenEmpty(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	// GroupID="" should default to Topic. NewConsumer is lazy (groups are created
	// per-worker in Consume), so it returns a non-nil consumer without a server.
	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:   "my-topic",
		GroupID: "", // will be defaulted to Topic
	})
	assert.NotNil(t, consumer)
}

func TestNewConsumerDefaultsConcurrencyToOne(t *testing.T) {
	cfg := kafka.Config{Brokers: []string{"localhost:19099"}}
	broker, err := kafka.New(cfg)
	require.NoError(t, err)

	// Concurrency <= 0 should default to 1 without panicking. NewConsumer is lazy
	// (groups created per-worker in Consume), so it returns a non-nil consumer.
	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "t",
		Concurrency: 0,
	})
	assert.NotNil(t, consumer)
}

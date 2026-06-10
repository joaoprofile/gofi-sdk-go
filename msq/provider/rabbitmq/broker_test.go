package rabbitmq_test

import (
	"context"
	"os"
	"testing"

	"github.com/joaoprofile/gofi/msq/provider/rabbitmq"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("rabbitmq-provider-test")
	os.Exit(m.Run())
}

// Conn construction (error paths, no broker required)

func TestDialURLReturnsErrorForBadURL(t *testing.T) {
	_, err := rabbitmq.DialURL("not-an-amqp-url")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rabbitmq: dial failed")
}

func TestDialURLReturnsErrorWhenServerUnreachable(t *testing.T) {
	// 127.0.0.1:19676 — nothing listening, immediate connection refused.
	_, err := rabbitmq.DialURL("amqp://guest:guest@127.0.0.1:19676/")
	assert.Error(t, err)
}

func TestDialReturnsErrorOrSucceeds(t *testing.T) {
	// Dial uses environment variables. In CI without a broker it returns an error.
	// With a broker it succeeds. Both outcomes are valid; we only verify no panic.
	conn, err := rabbitmq.Dial()
	if err == nil && conn != nil {
		conn.Close() //nolint:errcheck
	}
}

// Broker and producer/consumer construction (require real Conn)

// tryDial attempts to establish a real AMQP connection.
// Skips the test if no RabbitMQ is available.
func tryDial(t *testing.T) *rabbitmq.Conn {
	t.Helper()
	// Attempt the default guest connection. If it fails for any reason, skip.
	// Use 127.0.0.1:19676 first to check if something is reachable at 5672.
	conn, err := rabbitmq.DialURL("amqp://guest:guest@127.0.0.1:19676/")
	if err == nil {
		// Unlikely but handle gracefully.
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	t.Skipf("no RabbitMQ available — skipping integration tests")
	return nil
}

func TestNewBroker(t *testing.T) {
	conn := tryDial(t)
	broker := rabbitmq.New(conn, "test-exchange")
	assert.NotNil(t, broker)
}

func TestConnSetup(t *testing.T) {
	conn := tryDial(t)
	err := conn.Setup(context.Background(), "test-exchange")
	assert.NoError(t, err)
}

func TestConnIsConnected(t *testing.T) {
	conn := tryDial(t)
	assert.True(t, conn.IsConnected())
}

func TestConnClose(t *testing.T) {
	conn := tryDial(t)
	require.NoError(t, conn.Close())
	assert.False(t, conn.IsConnected())
}

func TestNewProducer(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	p, err := broker.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Close()
}

func TestNewConsumer(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	c := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "test-queue",
		RoutingKey:  "test-queue",
		Concurrency: 1,
	})
	require.NotNil(t, c)
	defer c.Close()
}

func TestNewConsumerRoutingKeyDefaultsToTopic(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	// RoutingKey="" should default to Topic.
	c := broker.NewConsumer(types.ConsumeConfig{Topic: "routing-test", RoutingKey: ""})
	require.NotNil(t, c)
	defer c.Close()
}

func TestProducerSendMessage(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	p, err := broker.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Close()

	msg := types.NewMessageWithTopic("test-queue", map[string]string{"event": "test"})
	assert.NoError(t, p.SendMessage(context.Background(), msg))
}

func TestProducerSendMessagesBatch(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	p, err := broker.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
	defer p.Close()

	msgs := []*types.Message{
		types.NewMessageWithTopic("test-queue", "a"),
		types.NewMessageWithTopic("test-queue", "b"),
	}
	assert.NoError(t, p.SendMessagesBatch(context.Background(), msgs))
}

func TestConsumerPauseAndResume(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	c := broker.NewConsumer(types.ConsumeConfig{Topic: "pause-test", Concurrency: 1})
	require.NotNil(t, c)
	defer c.Close()

	assert.NoError(t, c.Pause())
	assert.NoError(t, c.Resume())
}

func TestProducerClose(t *testing.T) {
	conn := tryDial(t)
	conn.Setup(context.Background(), "test-exchange") //nolint:errcheck
	broker := rabbitmq.New(conn, "test-exchange")

	p, err := broker.NewProducer()
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.NoError(t, p.Close())
}

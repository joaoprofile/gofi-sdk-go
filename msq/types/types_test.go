package types_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/msq/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Result

func TestResultConstants(t *testing.T) {
	assert.Equal(t, types.Result(0), types.Ack)
	assert.Equal(t, types.Result(1), types.Nack)
	assert.Equal(t, types.Result(2), types.Ignore)

	// all three must be distinct
	assert.NotEqual(t, types.Ack, types.Nack)
	assert.NotEqual(t, types.Ack, types.Ignore)
	assert.NotEqual(t, types.Nack, types.Ignore)
}

// ByteEncoder

func TestByteEncoderEncode(t *testing.T) {
	data := []byte("hello")
	enc := types.ByteEncoder(data)

	got, err := enc.Encode()
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestByteEncoderLength(t *testing.T) {
	assert.Equal(t, 5, types.ByteEncoder("hello").Length())
	assert.Equal(t, 0, types.ByteEncoder(nil).Length())
}

// Message

func TestNewMessageSetsIDAndTimestamp(t *testing.T) {
	before := time.Now()
	msg := types.NewMessage("payload")
	after := time.Now()

	assert.NotEqual(t, [16]byte{}, msg.Id)
	assert.False(t, msg.Timestamp.IsZero())
	assert.True(t, !msg.Timestamp.Before(before) && !msg.Timestamp.After(after))
}

func TestNewMessageSerializesValue(t *testing.T) {
	type body struct{ X int }
	msg := types.NewMessage(body{X: 42})

	var got body
	require.NoError(t, json.Unmarshal(msg.Value, &got))
	assert.Equal(t, 42, got.X)
}

func TestNewMessageInitializesHeaders(t *testing.T) {
	msg := types.NewMessage(nil)
	assert.NotNil(t, msg.Headers)
}

func TestNewMessageWithTopic(t *testing.T) {
	msg := types.NewMessageWithTopic("orders", "data")

	assert.Equal(t, "orders", msg.Topic)
	assert.NotNil(t, msg.Value)
}

func TestWithTopicChaining(t *testing.T) {
	msg := types.NewMessage("x")
	returned := msg.WithTopic("events")

	assert.Same(t, msg, returned, "WithTopic must return the same pointer")
	assert.Equal(t, "events", msg.Topic)
}

func TestWithKeyChaining(t *testing.T) {
	msg := types.NewMessage("x")
	returned := msg.WithKey("mykey")

	assert.Same(t, msg, returned)
	assert.Equal(t, "mykey", msg.Key)
}

func TestWithHeaderChaining(t *testing.T) {
	msg := types.NewMessage("x")
	returned := msg.WithHeader("trace-id", "abc123")

	assert.Same(t, msg, returned)
	assert.Equal(t, "abc123", msg.Headers["trace-id"])
}

func TestWithHeaderCreatesMapWhenNil(t *testing.T) {
	msg := &types.Message{} // no headers initialised
	msg.WithHeader("k", "v")

	assert.Equal(t, "v", msg.Headers["k"])
}

func TestWithHeaderMultiple(t *testing.T) {
	msg := types.NewMessage(nil)
	msg.WithHeader("a", "1").WithHeader("b", "2")

	assert.Equal(t, "1", msg.Headers["a"])
	assert.Equal(t, "2", msg.Headers["b"])
}

func TestStringReturnsValidJSON(t *testing.T) {
	msg := types.NewMessageWithTopic("t", map[string]int{"n": 7})
	s := msg.String()

	assert.Contains(t, s, `"topic":"t"`)
	// Must be parseable JSON
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(s), &raw))
}

func TestDecodeMessage(t *testing.T) {
	type payload struct{ Score float64 }
	msg := types.NewMessage(payload{Score: 9.5})

	var got payload
	require.NoError(t, msg.DecodeMessage(&got))
	assert.InDelta(t, 9.5, got.Score, 1e-9)
}

func TestDecodeMessageError(t *testing.T) {
	msg := &types.Message{Value: json.RawMessage(`not-json`)}
	var got struct{ X int }
	assert.Error(t, msg.DecodeMessage(&got))
}

func TestUnpackMessage(t *testing.T) {
	type order struct{ Amount int }
	msg := types.NewMessage(order{Amount: 100})

	got, err := types.UnpackMessage[order](msg)
	require.NoError(t, err)
	assert.Equal(t, 100, got.Amount)
}

func TestUnpackMessageError(t *testing.T) {
	msg := &types.Message{Value: json.RawMessage(`{invalid}`)}
	_, err := types.UnpackMessage[struct{ X int }](msg)
	assert.Error(t, err)
}

func TestUnpackMessageWrongType(t *testing.T) {
	type src struct{ Name string }
	type dst struct{ Age int }

	msg := types.NewMessage(src{Name: "Emilia"})

	got, err := types.UnpackMessage[dst](msg)
	// JSON decoding into mismatched struct succeeds (zero-value fields); no error
	require.NoError(t, err)
	assert.Equal(t, 0, got.Age)
}

// ConsumeConfig

func TestDefaultConsumeConfig(t *testing.T) {
	cfg := types.DefaultConsumeConfig("my-topic")

	assert.Equal(t, "my-topic", cfg.Topic)
	assert.Equal(t, types.DefaultConcurrency, cfg.Concurrency)
	assert.Equal(t, types.DefaultPollInterval, cfg.PollInterval)
}

func TestDefaultConsumeConfigConstants(t *testing.T) {
	assert.Greater(t, types.DefaultConcurrency, 0)
	assert.Greater(t, types.DefaultPollInterval, time.Duration(0))
}

// QueueAttributes

func TestQueueAttributesToConsumeConfig(t *testing.T) {
	qa := types.QueueAttributes{
		QueueName:  "wb.orders",
		QueueID:    "ocid1.queue.123",
		RoutingKey: "orders.created",
	}

	cfg := qa.ToConsumeConfig()

	assert.Equal(t, "wb.orders", cfg.Topic)
	assert.Equal(t, "ocid1.queue.123", cfg.QueueID)
	assert.Equal(t, "orders.created", cfg.RoutingKey)
	assert.Equal(t, types.DefaultConcurrency, cfg.Concurrency)
}

func TestQueueAttributesToConsumeConfigEmpty(t *testing.T) {
	cfg := types.QueueAttributes{}.ToConsumeConfig()

	assert.Empty(t, cfg.Topic)
	assert.Equal(t, types.DefaultConcurrency, cfg.Concurrency)
}

// BrokerEvent

func TestBrokerEventTypes(t *testing.T) {
	events := []types.BrokerEventType{
		types.EventMessageSent,
		types.EventMessageReceived,
		types.EventMessageAcked,
		types.EventMessageNacked,
		types.EventConsumerStarted,
		types.EventConsumerStopped,
		types.EventProducerError,
		types.EventConsumerError,
	}

	seen := make(map[types.BrokerEventType]bool)
	for _, e := range events {
		assert.False(t, seen[e], "duplicate event type: %s", e)
		seen[e] = true
		assert.NotEmpty(t, string(e))
	}
}

func TestBrokerEventFields(t *testing.T) {
	now := time.Now()
	ev := types.BrokerEvent{
		Type:      types.EventMessageSent,
		Topic:     "orders",
		MessageID: "abc-123",
		Timestamp: now,
	}

	assert.Equal(t, types.EventMessageSent, ev.Type)
	assert.Equal(t, "orders", ev.Topic)
	assert.Equal(t, "abc-123", ev.MessageID)
	assert.Equal(t, now, ev.Timestamp)
	assert.NoError(t, ev.Error)
}

package types_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/types"
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

// Message

func TestNewMessageSetsIDAndTimestamp(t *testing.T) {
	before := time.Now()
	msg := testMessage("payload")
	after := time.Now()

	assert.NotEqual(t, [16]byte{}, msg.Id)
	assert.False(t, msg.Timestamp.IsZero())
	assert.True(t, !msg.Timestamp.Before(before) && !msg.Timestamp.After(after))
}

func TestNewMessageSerializesValue(t *testing.T) {
	type body struct{ X int }
	msg := testMessage(body{X: 42})

	var got body
	require.NoError(t, json.Unmarshal(msg.Value, &got))
	assert.Equal(t, 42, got.X)
}

func TestNewMessageInitializesHeaders(t *testing.T) {
	msg := testMessage(nil)
	assert.NotNil(t, msg.Headers)
}

func TestNewMessageWithTopic(t *testing.T) {
	msg := testMessageWithTopic("orders", "data")

	assert.Equal(t, "orders", msg.Topic)
	assert.NotNil(t, msg.Value)
}

func TestWithTopicChaining(t *testing.T) {
	msg := testMessage("x")
	returned := msg.WithTopic("events")

	assert.Same(t, msg, returned, "WithTopic must return the same pointer")
	assert.Equal(t, "events", msg.Topic)
}

func TestWithKeyChaining(t *testing.T) {
	msg := testMessage("x")
	returned := msg.WithKey("mykey")

	assert.Same(t, msg, returned)
	assert.Equal(t, "mykey", msg.Key)
}

func TestWithHeaderChaining(t *testing.T) {
	msg := testMessage("x")
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
	msg := testMessage(nil)
	msg.WithHeader("a", "1").WithHeader("b", "2")

	assert.Equal(t, "1", msg.Headers["a"])
	assert.Equal(t, "2", msg.Headers["b"])
}

func TestStringReturnsValidJSON(t *testing.T) {
	msg := testMessageWithTopic("t", map[string]int{"n": 7})
	s := msg.String()

	assert.Contains(t, s, `"topic":"t"`)
	// Must be parseable JSON
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &raw))
}

func TestDecodeMessage(t *testing.T) {
	type payload struct{ Score float64 }
	msg := testMessage(payload{Score: 9.5})

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
	msg := testMessage(order{Amount: 100})

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

	msg := testMessage(src{Name: "Emilia"})

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
		types.EventMessageDeadLettered,
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

func TestConsumeConfigDeliveryLimit(t *testing.T) {
	cases := []struct {
		max, count int
		dlq        string
		limit      int
		reached    bool
	}{
		{0, 9, "dlq", types.DefaultMaxDeliveries, false},
		{0, types.DefaultMaxDeliveries, "dlq", types.DefaultMaxDeliveries, true},
		{0, 1000, "", 0, false}, // no DLQ: never dropped by default
		{3, 3, "", 3, true},     // an explicit limit applies without a DLQ
		{3, 0, "", 3, false},    // unknown count never reaches the limit
		{-1, 1000, "dlq", 0, false},
	}
	for _, c := range cases {
		cfg := types.ConsumeConfig{MaxDeliveries: c.max, DeadLetterTopic: c.dlq}
		if got := cfg.DeliveryLimit(); got != c.limit {
			t.Errorf("MaxDeliveries=%d: limit %d, want %d", c.max, got, c.limit)
		}
		if got := cfg.DeliveryLimitReached(c.count); got != c.reached {
			t.Errorf("MaxDeliveries=%d count=%d: reached %v, want %v", c.max, c.count, got, c.reached)
		}
	}
}

func TestConsumeConfigHandlerTimeout(t *testing.T) {
	if got := (types.ConsumeConfig{}).EffectiveHandlerTimeout(); got != types.DefaultHandlerTimeout {
		t.Errorf("zero must use the default, got %v", got)
	}
	if got := (types.ConsumeConfig{HandlerTimeout: -1}).EffectiveHandlerTimeout(); got != 0 {
		t.Errorf("negative must disable it, got %v", got)
	}
	if got := (types.ConsumeConfig{HandlerTimeout: time.Second}).EffectiveHandlerTimeout(); got != time.Second {
		t.Errorf("explicit value must be kept, got %v", got)
	}
}

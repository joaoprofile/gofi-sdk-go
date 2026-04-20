package redis_test

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi/msq/port"
	redisprovider "github.com/joaoprofile/gofi/msq/provider/redis"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	logging.NewLogger("redis-broker-test")
	os.Exit(m.Run())
}

// newTestBroker starts a miniredis server and returns a Broker backed by it.
func newTestBroker(t *testing.T) (*redisprovider.Broker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return redisprovider.NewWithClient(client), mr
}

//  Construction ---

func TestNewCreatesStandaloneBroker(t *testing.T) {
	mr := miniredis.RunT(t)
	b := redisprovider.New(redisprovider.Config{Addr: mr.Addr()})
	assert.NotNil(t, b)
}

func TestNewCreatesClusterBroker(t *testing.T) {
	mr := miniredis.RunT(t)
	// miniredis doesn't fully speak cluster but we can verify construction.
	b := redisprovider.New(redisprovider.Config{
		ClusterAddrs: []string{mr.Addr()},
	})
	assert.NotNil(t, b)
}

func TestNewWithClient(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	b := redisprovider.NewWithClient(client)
	assert.NotNil(t, b)
}

func TestNewProducerNotNil(t *testing.T) {
	broker, _ := newTestBroker(t)
	p := broker.NewProducer()
	assert.NotNil(t, p)
}

func TestNewConsumerNotNil(t *testing.T) {
	broker, _ := newTestBroker(t)
	c := broker.NewConsumer(types.ConsumeConfig{Topic: "ch", Concurrency: 1})
	assert.NotNil(t, c)
}

func TestNewConsumerDefaultsConcurrency(t *testing.T) {
	// concurrency=0 must not panic during construction.
	broker, _ := newTestBroker(t)
	c := broker.NewConsumer(types.ConsumeConfig{Topic: "ch", Concurrency: 0})
	assert.NotNil(t, c)
}

//  Producer

func TestProducerSendMessage(t *testing.T) {
	broker, mr := newTestBroker(t)
	producer := broker.NewProducer()
	defer producer.Close()

	msg := types.NewMessageWithTopic("events", map[string]string{"action": "signup"})

	// Subscribe before publishing so the message is received.
	sub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer sub.Close()
	pubsub := sub.Subscribe(context.Background(), "events")
	defer pubsub.Close()

	require.NoError(t, producer.SendMessage(context.Background(), msg))

	// Verify the message arrived.
	select {
	case received := <-pubsub.Channel():
		var got types.Message
		require.NoError(t, json.Unmarshal([]byte(received.Payload), &got))
		assert.Equal(t, "events", got.Topic)
	case <-time.After(2 * time.Second):
		t.Fatal("no message received within timeout")
	}
}

func TestProducerSendMessageMissingTopic(t *testing.T) {
	broker, _ := newTestBroker(t)
	producer := broker.NewProducer()
	defer producer.Close()

	err := producer.SendMessage(context.Background(), &types.Message{Topic: ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Topic")
}

func TestProducerSendMessagesBatch(t *testing.T) {
	broker, mr := newTestBroker(t)
	producer := broker.NewProducer()
	defer producer.Close()

	sub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer sub.Close()
	pubsub := sub.Subscribe(context.Background(), "batch-ch")
	defer pubsub.Close()

	msgs := []*types.Message{
		types.NewMessageWithTopic("batch-ch", "a"),
		types.NewMessageWithTopic("batch-ch", "b"),
		types.NewMessageWithTopic("batch-ch", "c"),
	}
	require.NoError(t, producer.SendMessagesBatch(context.Background(), msgs))

	count := 0
	timeout := time.After(2 * time.Second)
	ch := pubsub.Channel()
	for count < 3 {
		select {
		case <-ch:
			count++
		case <-timeout:
			t.Fatalf("expected 3 messages, got %d", count)
		}
	}
	assert.Equal(t, 3, count)
}

func TestProducerSendMessagesBatchMissingTopic(t *testing.T) {
	broker, _ := newTestBroker(t)
	producer := broker.NewProducer()
	defer producer.Close()

	msgs := []*types.Message{
		types.NewMessageWithTopic("ch", "ok"),
		{Topic: ""}, // missing topic
	}
	err := producer.SendMessagesBatch(context.Background(), msgs)
	assert.Error(t, err)
}

func TestProducerClose(t *testing.T) {
	broker, _ := newTestBroker(t)
	producer := broker.NewProducer()
	assert.NoError(t, producer.Close())
}

//  Consumer

func TestConsumerReceivesMessage(t *testing.T) {
	broker, mr := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{
		Topic:       "notifications",
		Concurrency: 1,
	})
	defer consumer.Close()

	var received types.Message
	receivedCh := make(chan struct{}, 1)

	handler := port.MessageHandlerFunc(func(_ context.Context, msg *types.Message) (types.Result, error) {
		received = *msg
		receivedCh <- struct{}{}
		return types.Ack, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		consumer.Consume(ctx, handler) //nolint:errcheck
	}()

	// Give subscriber time to attach.
	time.Sleep(50 * time.Millisecond)

	pub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer pub.Close()

	msg := types.NewMessageWithTopic("notifications", map[string]string{"event": "login"})
	payload, _ := json.Marshal(msg)
	require.NoError(t, pub.Publish(context.Background(), "notifications", string(payload)).Err())

	select {
	case <-receivedCh:
		assert.Equal(t, "notifications", received.Topic)
	case <-time.After(3 * time.Second):
		t.Fatal("handler was not called")
	}
}

func TestConsumerHandlesRawPayload(t *testing.T) {
	// Non-JSON payload must be wrapped as raw bytes value without crashing.
	broker, mr := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "raw-ch", Concurrency: 1})
	defer consumer.Close()

	receivedCh := make(chan []byte, 1)
	handler := port.MessageHandlerFunc(func(_ context.Context, msg *types.Message) (types.Result, error) {
		receivedCh <- msg.Value
		return types.Ack, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { consumer.Consume(ctx, handler) }() //nolint:errcheck

	time.Sleep(50 * time.Millisecond)

	pub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer pub.Close()
	pub.Publish(context.Background(), "raw-ch", "not-json-at-all")

	select {
	case val := <-receivedCh:
		assert.Equal(t, []byte("not-json-at-all"), val)
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called for raw payload")
	}
}

func TestConsumerNackIsLogged(t *testing.T) {
	// Nack must not crash; the consumer continues processing.
	broker, mr := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "nack-ch", Concurrency: 1})
	defer consumer.Close()

	called := make(chan struct{}, 1)
	handler := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		called <- struct{}{}
		return types.Nack, assert.AnError
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { consumer.Consume(ctx, handler) }() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)

	pub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer pub.Close()
	msg := types.NewMessageWithTopic("nack-ch", "data")
	payload, _ := json.Marshal(msg)
	pub.Publish(context.Background(), "nack-ch", string(payload))

	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestConsumerIgnoreResult(t *testing.T) {
	broker, mr := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "ignore-ch", Concurrency: 1})
	defer consumer.Close()

	called := make(chan struct{}, 1)
	handler := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		called <- struct{}{}
		return types.Ignore, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { consumer.Consume(ctx, handler) }() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)

	pub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer pub.Close()
	msg := types.NewMessageWithTopic("ignore-ch", "x")
	payload, _ := json.Marshal(msg)
	pub.Publish(context.Background(), "ignore-ch", string(payload))

	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestConsumerPauseAndResume(t *testing.T) {
	broker, mr := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "pause-ch", Concurrency: 1})
	defer consumer.Close()

	var handleCount atomic.Int32
	handler := port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
		handleCount.Add(1)
		return types.Ack, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { consumer.Consume(ctx, handler) }() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)

	// Pause — messages published while paused must be dropped.
	require.NoError(t, consumer.Pause())

	pub := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer pub.Close()

	msg := types.NewMessageWithTopic("pause-ch", "during-pause")
	payload, _ := json.Marshal(msg)
	pub.Publish(context.Background(), "pause-ch", string(payload))

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), handleCount.Load(), "message should not be handled while paused")

	// Resume — now send another message.
	require.NoError(t, consumer.Resume())
	time.Sleep(50 * time.Millisecond)

	msg2 := types.NewMessageWithTopic("pause-ch", "after-resume")
	payload2, _ := json.Marshal(msg2)
	pub.Publish(context.Background(), "pause-ch", string(payload2))

	require.Eventually(t, func() bool {
		return handleCount.Load() == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestConsumerCancelContextStops(t *testing.T) {
	broker, _ := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "cancel-ch", Concurrency: 1})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- consumer.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, _ *types.Message) (types.Result, error) {
			return types.Ack, nil
		}))
	}()

	// Allow Consume to start then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Consume did not return after context cancellation")
	}
}

func TestConsumerClose(t *testing.T) {
	broker, _ := newTestBroker(t)
	consumer := broker.NewConsumer(types.ConsumeConfig{Topic: "ch"})
	assert.NoError(t, consumer.Close())
}

// Producer error paths

func TestProducerSendMessagePublishError(t *testing.T) {
	// Close the server after creating the producer so Publish fails.
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	broker := redisprovider.NewWithClient(client)
	producer := broker.NewProducer()
	defer producer.Close()

	mr.Close()

	msg := types.NewMessageWithTopic("events", "data")
	err := producer.SendMessage(context.Background(), msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "publish")
}

func TestProducerSendMessagesBatchPipelineError(t *testing.T) {
	// Close the server so the pipeline Exec fails.
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	broker := redisprovider.NewWithClient(client)
	producer := broker.NewProducer()
	defer producer.Close()

	mr.Close()

	msgs := []*types.Message{
		types.NewMessageWithTopic("ch", "a"),
		types.NewMessageWithTopic("ch", "b"),
	}
	err := producer.SendMessagesBatch(context.Background(), msgs)
	assert.Error(t, err)
}

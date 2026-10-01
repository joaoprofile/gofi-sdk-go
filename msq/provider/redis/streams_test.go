package redis_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	redisprovider "github.com/joaoprofile/gofi-sdk-go/msq/provider/redis"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStreamBroker(t *testing.T, cfg redisprovider.Config) (*redisprovider.Broker, goredis.UniversalClient) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	cfg.Mode = redisprovider.ModeStreams
	return redisprovider.NewWithClient(client, cfg), client
}

func consume(t *testing.T, b *redisprovider.Broker, cfg types.ConsumeConfig, h port.MessageHandler) port.Consumer {
	t.Helper()
	c, err := b.NewConsumer(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Consume(ctx, h) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	return c
}

func TestStreams_ProducerAppendsWithMaxLen(t *testing.T) {
	b, client := newStreamBroker(t, redisprovider.Config{StreamMaxLen: 100})
	p, err := b.NewProducer()
	require.NoError(t, err)
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "a")))
	require.NoError(t, p.SendMessagesBatch(context.Background(), []*types.Message{
		testMessageWithTopic("orders", "b"), testMessageWithTopic("orders", "c"),
	}))
	n, err := client.XLen(context.Background(), "orders").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	assert.Error(t, p.SendMessage(context.Background(), &types.Message{}), "topic is required")
}

// Unlike Pub/Sub, messages sent before the consumer starts are not lost.
func TestStreams_DeliversBacklogAndAcks(t *testing.T) {
	b, client := newStreamBroker(t, redisprovider.Config{})
	p, _ := b.NewProducer()
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "early")))

	got := make(chan string, 1)
	consume(t, b, types.ConsumeConfig{Topic: "orders", GroupID: "billing", InitialOffset: types.OffsetResetEarliest},
		port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			v, _ := types.UnpackMessage[string](m)
			got <- *v
			return types.Ack, nil
		}))

	select {
	case v := <-got:
		assert.Equal(t, "early", v)
	case <-time.After(3 * time.Second):
		t.Fatal("backlog not delivered")
	}
	require.Eventually(t, func() bool {
		p, err := client.XPending(context.Background(), "orders", "billing").Result()
		return err == nil && p.Count == 0
	}, 2*time.Second, 10*time.Millisecond, "acked entries leave the pending list")
}

func TestStreams_NackIsRedelivered(t *testing.T) {
	b, _ := newStreamBroker(t, redisprovider.Config{ClaimIdle: 50 * time.Millisecond})
	var calls atomic.Int32
	consume(t, b, types.ConsumeConfig{Topic: "orders", Concurrency: 1},
		port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
			if calls.Add(1) == 1 {
				return types.Nack, nil
			}
			return types.Ack, nil
		}))

	p, _ := b.NewProducer()
	require.Eventually(t, func() bool {
		// The group reads from "$": send until the consumer has joined.
		if calls.Load() == 0 {
			_ = p.SendMessage(context.Background(), testMessageWithTopic("orders", "v"))
		}
		return calls.Load() >= 2
	}, 5*time.Second, 100*time.Millisecond)
}

func TestStreams_UnknownMode(t *testing.T) {
	mr := miniredis.RunT(t)
	b := redisprovider.NewWithClient(goredis.NewClient(&goredis.Options{Addr: mr.Addr()}), redisprovider.Config{Mode: "kafka"})
	_, err := b.NewConsumer(types.ConsumeConfig{Topic: "orders"})
	assert.Error(t, err)
}

func TestStreams_PauseResume(t *testing.T) {
	b, _ := newStreamBroker(t, redisprovider.Config{})
	c, err := b.NewConsumer(types.ConsumeConfig{Topic: "orders"})
	require.NoError(t, err)
	assert.NoError(t, c.Pause())
	assert.NoError(t, c.Resume())
	assert.NoError(t, c.Close())
}

// An entry whose handler outlives ClaimIdle must not be claimed while it runs,
// neither by another consumer of the group nor by its own consumer.
func TestStreams_SlowHandlerIsNotReclaimed(t *testing.T) {
	b, _ := newStreamBroker(t, redisprovider.Config{})
	var deliveries atomic.Int32
	slow := port.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		deliveries.Add(1)
		time.Sleep(700 * time.Millisecond) // > 4x ClaimIdle
		return types.Ack, nil
	})
	cfg := types.ConsumeConfig{Topic: "orders", GroupID: "billing", Concurrency: 2,
		InitialOffset: types.OffsetResetEarliest, VisibilityTimeout: 150 * time.Millisecond}
	p, _ := b.NewProducer()
	require.NoError(t, p.SendMessage(context.Background(), testMessageWithTopic("orders", "v")))
	consume(t, b, cfg, slow)
	consume(t, b, cfg, slow)

	require.Eventually(t, func() bool { return deliveries.Load() >= 1 }, 3*time.Second, 10*time.Millisecond)
	time.Sleep(1200 * time.Millisecond)
	assert.Equal(t, int32(1), deliveries.Load())
}

// A reclaimed entry carries XPENDING's delivery count, keeps its Id and, once
// the pipeline rejects it at the limit, is acked instead of reclaimed forever.
func TestStreams_DeliveryCountAndRejectAcks(t *testing.T) {
	b, client := newStreamBroker(t, redisprovider.Config{ClaimIdle: 50 * time.Millisecond})
	var mu sync.Mutex
	var counts []int
	var ids []string
	consume(t, b, types.ConsumeConfig{Topic: "orders", GroupID: "g", Concurrency: 1, InitialOffset: types.OffsetResetEarliest},
		port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			mu.Lock()
			defer mu.Unlock()
			counts = append(counts, m.DeliveryCount)
			ids = append(ids, m.Id.String())
			if m.DeliveryCount >= 3 {
				return types.Reject, nil
			}
			return types.Nack, nil
		}))
	_, err := client.XAdd(context.Background(), &goredis.XAddArgs{Stream: "orders", Values: []any{"m", `raw`}}).Result()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		p, err := client.XPending(context.Background(), "orders", "g").Result()
		mu.Lock()
		defer mu.Unlock()
		return err == nil && p.Count == 0 && len(counts) >= 3
	}, 5*time.Second, 20*time.Millisecond, "a rejected entry leaves the pending list")
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int{1, 2, 3}, counts)
	assert.Equal(t, ids[0], ids[2], "a foreign entry keeps its Id across redeliveries")
}

// The stream name wins over a topic spoofed in the payload.
func TestStreams_TopicIsTheStream(t *testing.T) {
	b, client := newStreamBroker(t, redisprovider.Config{})
	got := make(chan string, 1)
	consume(t, b, types.ConsumeConfig{Topic: "orders", InitialOffset: types.OffsetResetEarliest},
		port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			got <- m.Topic
			return types.Ack, nil
		}))
	body, _ := json.Marshal(testMessageWithTopic("evil-"+strings.Repeat("x", 8), "v"))
	_, err := client.XAdd(context.Background(), &goredis.XAddArgs{Stream: "orders", Values: []any{"m", body}}).Result()
	require.NoError(t, err)
	select {
	case topic := <-got:
		assert.Equal(t, "orders", topic)
	case <-time.After(5 * time.Second):
		t.Fatal("entry not delivered")
	}
}

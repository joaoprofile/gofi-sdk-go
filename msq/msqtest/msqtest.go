// Package msqtest is the contract every msq provider must satisfy. Provider
// modules run it against in-process fakes and, in integration runs, against
// real brokers.
package msqtest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/google/uuid"
)

// Capabilities describes provider semantics the contract adapts to.
type Capabilities struct {
	// Redelivery: a Nack is delivered again (false for Redis Pub/Sub).
	Redelivery bool
	// DeliveryCount: Message.DeliveryCount grows on each redelivery.
	DeliveryCount bool
}

// Target is a broker under test.
type Target struct {
	Broker port.Broker
	Caps   Capabilities
	// Topic returns a fresh, empty topic/queue for each case.
	Topic func(t *testing.T) string
	// Config adapts the consume config (e.g. QueueID for OCI).
	Config func(cfg types.ConsumeConfig) types.ConsumeConfig
	// Timeout bounds each case (default 20s).
	Timeout time.Duration
}

// Run exercises the target.
func Run(t *testing.T, tg Target) {
	t.Helper()
	if tg.Timeout == 0 {
		tg.Timeout = 20 * time.Second
	}
	if tg.Config == nil {
		tg.Config = func(c types.ConsumeConfig) types.ConsumeConfig { return c }
	}

	t.Run("RoundTripPreservesMessage", func(t *testing.T) {
		topic := tg.Topic(t)
		want, _ := types.NewMessageWithTopic(topic, map[string]any{"n": 1})
		want.WithHeader("tenant", "a")
		got := consumeOne(t, tg, topic, []*types.Message{want}, func(*types.Message) types.Result { return types.Ack })
		if got[want.Id] == nil {
			t.Fatalf("message %s not received", want.Id)
		}
		m := got[want.Id]
		if string(m.Value) != string(want.Value) {
			t.Errorf("Value=%s, want %s", m.Value, want.Value)
		}
		if m.Headers["tenant"] != "a" {
			t.Errorf("Headers=%v", m.Headers)
		}
		if m.Topic != topic {
			t.Errorf("Topic=%q, want the transport destination %q", m.Topic, topic)
		}
	})

	t.Run("BatchIsDelivered", func(t *testing.T) {
		topic := tg.Topic(t)
		var msgs []*types.Message
		for i := range 5 {
			m, _ := types.NewMessageWithTopic(topic, i)
			msgs = append(msgs, m)
		}
		got := consumeOne(t, tg, topic, msgs, func(*types.Message) types.Result { return types.Ack })
		for _, m := range msgs {
			if got[m.Id] == nil {
				t.Errorf("message %s missing from batch", m.Id)
			}
		}
	})

	if tg.Caps.Redelivery {
		t.Run("NackIsRedelivered", func(t *testing.T) {
			topic := tg.Topic(t)
			m, _ := types.NewMessageWithTopic(topic, "retry")
			var mu sync.Mutex
			var counts []int
			got := consumeOne(t, tg, topic, []*types.Message{m}, func(d *types.Message) types.Result {
				mu.Lock()
				defer mu.Unlock()
				counts = append(counts, d.DeliveryCount)
				if len(counts) == 1 {
					return types.Nack
				}
				return types.Ack
			}, 2)
			if got[m.Id] == nil {
				t.Fatal("message not redelivered after Nack")
			}
			mu.Lock()
			defer mu.Unlock()
			if tg.Caps.DeliveryCount && (len(counts) < 2 || counts[0] < 1 || counts[1] <= counts[0]) {
				t.Errorf("DeliveryCount must grow on redelivery: %v", counts)
			}
		})
	}

	t.Run("PauseResume", func(t *testing.T) {
		c, err := tg.Broker.NewConsumer(tg.Config(types.ConsumeConfig{Topic: tg.Topic(t), Concurrency: 1}))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if err := c.Pause(); err != nil {
			t.Errorf("Pause: %v", err)
		}
		if err := c.Resume(); err != nil {
			t.Errorf("Resume: %v", err)
		}
	})
}

// consumeOne starts a consumer, sends msgs until every one has been handled
// `deliveries` times (default 1) and returns the last copy of each by Id.
// Sends repeat because some brokers only deliver to already-joined consumers.
func consumeOne(t *testing.T, tg Target, topic string, msgs []*types.Message, handle func(*types.Message) types.Result, deliveries ...int) map[uuid.UUID]*types.Message {
	t.Helper()
	need := 1
	if len(deliveries) > 0 {
		need = deliveries[0]
	}
	c, err := tg.Broker.NewConsumer(tg.Config(types.ConsumeConfig{
		Topic: topic, GroupID: topic, Concurrency: 2, InitialOffset: types.OffsetResetEarliest,
		PollInterval: time.Second,
	}))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), tg.Timeout)
	defer cancel()

	var mu sync.Mutex
	got := map[uuid.UUID]*types.Message{}
	counts := map[uuid.UUID]int{}
	done := make(chan struct{})
	consumed := make(chan error, 1)
	go func() {
		consumed <- c.Consume(ctx, port.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
			res := handle(m)
			mu.Lock()
			defer mu.Unlock()
			counts[m.Id]++
			got[m.Id] = m
			complete := true
			for _, w := range msgs {
				if counts[w.Id] < need {
					complete = false
				}
			}
			if complete {
				select {
				case <-done:
				default:
					close(done)
				}
			}
			return res, nil
		}))
	}()

	p, err := tg.Broker.NewProducer()
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	defer p.Close()
	send := func(batch []*types.Message) {
		var err error
		if len(batch) == 1 {
			err = p.SendMessage(ctx, batch[0])
		} else {
			err = p.SendMessagesBatch(ctx, batch)
		}
		if err != nil && ctx.Err() == nil {
			t.Errorf("send: %v", err)
		}
	}
	send(msgs)
	resend := time.NewTicker(2 * time.Second)
	defer resend.Stop()
	for waiting := true; waiting; {
		select {
		case err := <-consumed:
			// Consume ended before every message arrived.
			t.Fatalf("Consume returned early: %v", err)
		case <-done:
			waiting = false
		case <-resend.C:
			// The consumer may have joined after the send: resend only what
			// never arrived, so a second delivery of a Nack is a redelivery.
			var missing []*types.Message
			mu.Lock()
			for _, m := range msgs {
				if counts[m.Id] == 0 {
					missing = append(missing, m)
				}
			}
			mu.Unlock()
			if len(missing) > 0 {
				send(missing)
			}
		case <-ctx.Done():
			waiting = false
		}
	}
	cancel()
	<-consumed
	_ = c.Close()
	mu.Lock()
	defer mu.Unlock()
	return got
}

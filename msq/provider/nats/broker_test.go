package nats_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq"
	"github.com/gofi-labs/gofi-sdk-go/msq/msqtest"
	natsprovider "github.com/gofi-labs/gofi-sdk-go/msq/provider/nats"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
	"github.com/nats-io/nats-server/v2/server"
)

func TestMain(m *testing.M) {
	logging.NewLogger("nats-test")
	os.Exit(m.Run())
}

// url starts an embedded JetStream server unless GOFI_IT_NATS_URL is set.
func url(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("GOFI_IT_NATS_URL"); u != "" {
		return u
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func newBroker(t *testing.T) *natsprovider.Broker {
	t.Helper()
	b, err := natsprovider.New(natsprovider.Config{URL: url(t), Name: "gofi-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	if err := b.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestContract(t *testing.T) {
	msqtest.Run(t, msqtest.Target{
		Broker: newBroker(t),
		Caps:   msqtest.Capabilities{Redelivery: true, DeliveryCount: true},
		Topic:  func(*testing.T) string { return "it." + uuid.NewString()[:8] },
	})
}

// Resending the same Id is de-duplicated by the stream.
func TestDuplicateIDsAreStoredOnce(t *testing.T) {
	b := newBroker(t)
	p, _ := b.NewProducer()
	m, _ := types.NewMessageWithTopic("dedup."+uuid.NewString()[:8], "v")
	for range 3 {
		if err := p.SendMessage(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	got := 0
	c, _ := b.NewConsumer(types.ConsumeConfig{Topic: m.Topic, InitialOffset: types.OffsetResetEarliest})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Consume(ctx, portFunc(func(*types.Message) { got++ }))
	if got != 1 {
		t.Errorf("delivered %d times, want 1", got)
	}
}

func TestSetupFailsWithoutJetStream(t *testing.T) {
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	s.ReadyForConnections(10 * time.Second)
	defer s.Shutdown()
	b, err := natsprovider.New(natsprovider.Config{URL: s.ClientURL()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Setup(context.Background()); err == nil {
		t.Error("Setup must fail when JetStream is disabled")
	}
}

func TestConsumerRequiresTopic(t *testing.T) {
	if _, err := newBroker(t).NewConsumer(types.ConsumeConfig{}); err == nil {
		t.Error("empty topic must fail")
	}
}

// A handler slower than AckWait keeps its message: InProgress renews the
// lease, so it is not redelivered to a second worker while still running.
func TestSlowHandlerIsNotRedeliveredWhileRunning(t *testing.T) {
	b := newBroker(t)
	topic := "slow." + uuid.NewString()[:8]
	p, _ := b.NewProducer()
	m, _ := types.NewMessageWithTopic(topic, "v")
	if err := p.SendMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	c, _ := b.NewConsumer(types.ConsumeConfig{
		Topic: topic, Concurrency: 4, InitialOffset: types.OffsetResetEarliest, VisibilityTimeout: time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var deliveries atomic.Int32
	_ = c.Consume(ctx, msq.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		deliveries.Add(1)
		time.Sleep(2500 * time.Millisecond) // 2.5x AckWait
		return types.Ack, nil
	}))
	if n := deliveries.Load(); n != 1 {
		t.Fatalf("delivered %d times, want 1", n)
	}
}

// A message nacked on every delivery is terminated by the pipeline's limit and
// the durable carries MaxDeliver as a server-side backstop.
func TestPoisonRejectedAfterMaxDeliveries(t *testing.T) {
	b := newBroker(t)
	topic := "poison." + uuid.NewString()[:8]
	rejected := make(chan msq.BrokerEvent, 1)
	svc, _ := msq.New(msq.Config{Broker: b, BrokerType: msq.BrokerNATS, OnEvent: func(_ context.Context, e msq.BrokerEvent) {
		if e.Type == msq.EventMessageRejected {
			rejected <- e
		}
	}})
	p, _ := b.NewProducer()
	m, _ := types.NewMessageWithTopic(topic, "poison")
	if err := p.SendMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	c, _ := svc.NewConsumer(types.ConsumeConfig{Topic: topic, InitialOffset: types.OffsetResetEarliest, MaxDeliveries: 3, RetryBackoff: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var counts []int
	var ids []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, got *types.Message) (types.Result, error) {
			counts = append(counts, got.DeliveryCount)
			ids = append(ids, got.Id.String())
			return types.Nack, errors.New("poison")
		}))
	}()
	select {
	case <-rejected:
	case <-ctx.Done():
		t.Fatal("poison message never rejected")
	}
	time.Sleep(1500 * time.Millisecond) // a Nak instead of Term would redeliver it
	cancel()
	<-done
	if len(counts) != 3 || counts[2] != 3 || ids[0] != ids[2] {
		t.Fatalf("deliveries = %v ids = %v, want [1 2 3] with one id", counts, ids)
	}
}

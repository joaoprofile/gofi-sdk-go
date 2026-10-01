package kafka_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq"
	"github.com/gofi-labs/gofi-sdk-go/msq/msqtest"
	"github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka"
	"github.com/google/uuid"
)

// Set GOFI_IT_KAFKA_BROKERS (comma separated) to run it.
func TestContract(t *testing.T) {
	brokers := os.Getenv("GOFI_IT_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("GOFI_IT_KAFKA_BROKERS not set")
	}
	b, err := kafka.New(kafka.Config{Brokers: strings.Split(brokers, ",")})
	if err != nil {
		t.Fatal(err)
	}
	msqtest.Run(t, msqtest.Target{
		Broker:  b,
		Caps:    msqtest.Capabilities{Redelivery: true, DeliveryCount: true}, // nacks are redelivered in place
		Timeout: 60 * time.Second,
		Topic:   func(t *testing.T) string { return itTopic(t, brokers) },
	})
}

// A record whose retries are exhausted is dead-lettered with its key (same
// partitioning as the original) and only then committed.
func TestIntegration_DeadLetterKeepsKey(t *testing.T) {
	brokers := os.Getenv("GOFI_IT_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("GOFI_IT_KAFKA_BROKERS not set")
	}
	b, err := kafka.New(kafka.Config{Brokers: strings.Split(brokers, ",")})
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := msq.New(msq.Config{Broker: b, BrokerType: msq.BrokerKafka})
	topic, dlq := itTopic(t, brokers), itTopic(t, brokers)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p, err := b.NewProducer()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	m, _ := msq.NewMessageWithTopic(topic, "poison")
	m.WithKey("customer-7")
	if err := p.SendMessage(ctx, m); err != nil {
		t.Fatal(err)
	}

	dead := make(chan *msq.Message, 1)
	dc, _ := b.NewConsumer(msq.ConsumeConfig{Topic: dlq, InitialOffset: msq.OffsetResetEarliest})
	go dc.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, m *msq.Message) (msq.Result, error) {
		dead <- m
		return msq.Ack, nil
	}))
	c, _ := svc.NewConsumer(msq.ConsumeConfig{Topic: topic, InitialOffset: msq.OffsetResetEarliest, DeadLetterTopic: dlq})
	go c.Consume(ctx, msq.MessageHandlerFunc(func(context.Context, *msq.Message) (msq.Result, error) {
		return msq.Nack, errors.New("poison")
	}))
	select {
	case got := <-dead:
		if got.Id != m.Id || got.Key != "customer-7" {
			t.Fatalf("dead letter = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("record never dead-lettered")
	}
}

// itTopic creates a fresh topic: brokers may have auto-creation disabled.
func itTopic(t *testing.T, brokers string) string {
	t.Helper()
	name := "it-" + uuid.NewString()[:8]
	b, err := kafka.New(kafka.Config{Brokers: strings.Split(brokers, ","), Topics: []kafka.TopicConfig{{Name: name}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	return name
}

// A poison record without a dead-letter topic is rejected after MaxDeliveries
// in-place deliveries, so the partition moves on instead of blocking forever.
func TestIntegration_PoisonRejectedAfterMaxDeliveries(t *testing.T) {
	brokers := os.Getenv("GOFI_IT_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("GOFI_IT_KAFKA_BROKERS not set")
	}
	b, err := kafka.New(kafka.Config{Brokers: strings.Split(brokers, ",")})
	if err != nil {
		t.Fatal(err)
	}
	rejected := make(chan msq.BrokerEvent, 1)
	svc, _ := msq.New(msq.Config{Broker: b, BrokerType: msq.BrokerKafka, OnEvent: func(_ context.Context, e msq.BrokerEvent) {
		if e.Type == msq.EventMessageRejected {
			rejected <- e
		}
	}})
	topic := itTopic(t, brokers)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	p, err := b.NewProducer()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	poison, _ := msq.NewMessageWithTopic(topic, "poison")
	next, _ := msq.NewMessageWithTopic(topic, "next")
	if err := p.SendMessagesBatch(ctx, []*msq.Message{poison, next}); err != nil {
		t.Fatal(err)
	}

	after := make(chan int, 1)
	var deliveries atomic.Int32
	c, _ := svc.NewConsumer(msq.ConsumeConfig{Topic: topic, InitialOffset: msq.OffsetResetEarliest, MaxDeliveries: 2, RetryBackoff: time.Millisecond})
	go c.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, m *msq.Message) (msq.Result, error) {
		if m.Id == next.Id {
			after <- int(deliveries.Load())
			return msq.Ack, nil
		}
		deliveries.Add(1)
		return msq.Nack, errors.New("poison")
	}))
	select {
	case n := <-after:
		if n != 2 {
			t.Fatalf("poison delivered %d times, want 2", n)
		}
		if ev := <-rejected; ev.MessageID != poison.Id.String() {
			t.Fatalf("rejected event for %s", ev.MessageID)
		}
	case <-ctx.Done():
		t.Fatal("partition stayed blocked by the poison record")
	}
}

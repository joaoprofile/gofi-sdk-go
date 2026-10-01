package rabbitmq_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi-sdk-go/msq"
	"github.com/joaoprofile/gofi-sdk-go/msq/msqtest"
	"github.com/joaoprofile/gofi-sdk-go/msq/provider/rabbitmq"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/rabbitmq/amqp091-go"
)

// Set GOFI_IT_AMQP_URL (e.g. amqp://guest:guest@localhost:5672/) to run it.
func TestContract(t *testing.T) {
	url := os.Getenv("GOFI_IT_AMQP_URL")
	if url == "" {
		t.Skip("GOFI_IT_AMQP_URL not set")
	}
	for _, enc := range []types.Encoding{types.EncodingEnvelope, types.EncodingCloudEvents} {
		t.Run(string(enc), func(t *testing.T) {
			conn, err := rabbitmq.DialURL(url)
			if err != nil {
				t.Fatal(err)
			}
			b := rabbitmq.New(conn, "gofi-it", rabbitmq.WithEncoding(enc))
			t.Cleanup(func() { b.Close() })
			if err := b.Setup(context.Background()); err != nil {
				t.Fatal(err)
			}
			msqtest.Run(t, msqtest.Target{
				Broker: b,
				Caps:   msqtest.Capabilities{Redelivery: true, DeliveryCount: true},
				Topic:  func(*testing.T) string { return "it-" + uuid.NewString()[:8] },
			})
		})
	}
}

// A message no queue is bound for must fail the send, not vanish after a
// positive confirm.
func TestIntegration_UnroutableFailsSend(t *testing.T) {
	b := itBroker(t)
	p, err := b.NewProducer()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	m, _ := types.NewMessageWithTopic("it-nobody-"+uuid.NewString()[:8], "lost")
	if err := p.SendMessage(context.Background(), m); !errors.Is(err, rabbitmq.ErrUnroutable) {
		t.Fatalf("SendMessage = %v, want ErrUnroutable", err)
	}
	ok, _ := types.NewMessageWithTopic("it-nobody-"+uuid.NewString()[:8], "lost")
	if err := p.SendMessagesBatch(context.Background(), []*types.Message{ok}); !errors.Is(err, rabbitmq.ErrUnroutable) {
		t.Fatalf("SendMessagesBatch = %v, want ErrUnroutable", err)
	}
}

// A keyed message that exhausts its retries reaches the dead-letter queue:
// routing by Key used to send the copy nowhere and ack the original.
func TestIntegration_KeyedMessageIsDeadLettered(t *testing.T) {
	b := itBroker(t)
	svc, err := msq.New(msq.Config{Broker: b, BrokerType: msq.BrokerRabbitMQ})
	if err != nil {
		t.Fatal(err)
	}
	topic, dlq := "it-"+uuid.NewString()[:8], "it-dlq-"+uuid.NewString()[:8]
	dead := make(chan *types.Message, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dc, err := b.NewConsumer(types.ConsumeConfig{Topic: dlq, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	go dc.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		dead <- m
		return types.Ack, nil
	}))
	c, err := svc.NewConsumer(types.ConsumeConfig{Topic: topic, Concurrency: 1, DeadLetterTopic: dlq})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go c.Consume(ctx, msq.MessageHandlerFunc(func(context.Context, *types.Message) (types.Result, error) {
		return types.Nack, errors.New("poison")
	}))

	p, _ := b.NewProducer()
	defer p.Close()
	m, _ := types.NewMessageWithTopic(topic, "poison")
	m.WithKey("customer-7")
	if err := p.SendMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-dead:
		if got.Id != m.Id || got.Key != "customer-7" || got.Headers[msq.HeaderDLQOriginalTopic] != topic {
			t.Fatalf("dead letter = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("keyed message never reached the dead-letter queue")
	}
}

func itBroker(t *testing.T) *rabbitmq.Broker {
	t.Helper()
	url := os.Getenv("GOFI_IT_AMQP_URL")
	if url == "" {
		t.Skip("GOFI_IT_AMQP_URL not set")
	}
	conn, err := rabbitmq.DialURL(url)
	if err != nil {
		t.Fatal(err)
	}
	b := rabbitmq.New(conn, "gofi-it")
	t.Cleanup(func() { b.Close() })
	if err := b.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b
}

// A message that keeps failing without a dead-letter topic is rejected (not
// requeued) once MaxDeliveries is reached.
func TestIntegration_PoisonRejectedAfterMaxDeliveries(t *testing.T) {
	b := itBroker(t)
	rejected := make(chan msq.BrokerEvent, 1)
	svc, err := msq.New(msq.Config{Broker: b, BrokerType: msq.BrokerRabbitMQ, OnEvent: func(_ context.Context, e msq.BrokerEvent) {
		if e.Type == msq.EventMessageRejected {
			rejected <- e
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	topic := "it-" + uuid.NewString()[:8]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var deliveries []int
	var mu sync.Mutex
	c, err := svc.NewConsumer(types.ConsumeConfig{Topic: topic, Concurrency: 1, MaxDeliveries: 3, RetryBackoff: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go c.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		mu.Lock()
		deliveries = append(deliveries, m.DeliveryCount)
		mu.Unlock()
		return types.Nack, errors.New("poison")
	}))

	p, _ := b.NewProducer()
	defer p.Close()
	m, _ := types.NewMessageWithTopic(topic, "poison")
	if err := p.SendMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-rejected:
		if ev.MessageID != m.Id.String() {
			t.Fatalf("rejected %s", ev.MessageID)
		}
	case <-ctx.Done():
		t.Fatal("poison message never rejected")
	}
	time.Sleep(500 * time.Millisecond) // a requeued copy would be delivered again
	mu.Lock()
	defer mu.Unlock()
	if len(deliveries) != 3 || deliveries[2] != 3 {
		t.Fatalf("deliveries = %v, want [1 2 3]", deliveries)
	}
}

// Dial errors never echo the password, even for a reachable broker that
// refuses the credentials.
func TestIntegration_AuthFailureDoesNotLeakPassword(t *testing.T) {
	raw := os.Getenv("GOFI_IT_AMQP_URL")
	if raw == "" {
		t.Skip("GOFI_IT_AMQP_URL not set")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("nobody", "s3cret-wrong")
	_, err = rabbitmq.DialURL(u.String())
	if err == nil {
		t.Fatal("wrong credentials accepted")
	}
	if strings.Contains(err.Error(), "s3cret-wrong") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

// Quorum queues count deliveries themselves (x-delivery-count); the consumer
// reads that count, so the limit holds across consumers and restarts.
func TestIntegration_QuorumDeliveryCount(t *testing.T) {
	raw := os.Getenv("GOFI_IT_AMQP_URL")
	b := itBroker(t)
	topic := "it-q-" + uuid.NewString()[:8]
	conn, err := amqp091.Dial(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueueDeclare(topic, true, false, false, false, amqp091.Table{"x-queue-type": "quorum"}); err != nil {
		t.Fatal(err)
	}
	defer ch.QueueDelete(topic, false, false, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	counts := make(chan int, 10)
	c, err := b.NewConsumer(types.ConsumeConfig{Topic: topic, Concurrency: 1, RetryBackoff: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go c.Consume(ctx, msq.MessageHandlerFunc(func(_ context.Context, m *types.Message) (types.Result, error) {
		counts <- m.DeliveryCount
		if m.DeliveryCount < 3 {
			return types.Nack, nil
		}
		return types.Ack, nil
	}))
	p, _ := b.NewProducer()
	defer p.Close()
	m, _ := types.NewMessageWithTopic(topic, "q")
	if err := p.SendMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 3; want++ {
		select {
		case got := <-counts:
			if got != want {
				t.Fatalf("delivery %d reported DeliveryCount %d", want, got)
			}
		case <-ctx.Done():
			t.Fatal("message not redelivered")
		}
	}
}

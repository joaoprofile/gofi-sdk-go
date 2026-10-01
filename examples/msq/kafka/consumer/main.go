// Command consumer is a service of its own: it processes the OrderCreated
// events from Kafka, retries the ones that fail and dead-letters them.
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/messaging"
	"github.com/gofi-labs/gofi-sdk-go/msq"
	_ "github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka" // enables MESSAGING_PROVIDER=kafka
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

const (
	topic    = "orders"
	dlqTopic = "orders-dlq"
)

// OrderCreated is this service's copy of the event contract.
type OrderCreated struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

var errInvalidAmount = errors.New("amount must be greater than zero")

func main() {
	mq := messaging.New() // MESSAGING_* → Kafka
	svc, err := gofi.New("order-consumer").With(mq).Build()
	if err != nil {
		log.Fatal(err)
	}

	cfg := msq.DefaultConsumeConfig(topic)
	cfg.GroupID = "order-consumer"              // instances of this group share the partitions
	cfg.InitialOffset = msq.OffsetResetEarliest // first run reads the topic from the beginning
	cfg.MaxRetries = 2                          // a Nack is retried twice, with backoff...
	cfg.RetryBackoff = 500 * time.Millisecond
	cfg.DeadLetterTopic = dlqTopic // ...then a copy goes to the dead-letter topic

	dlq := msq.DefaultConsumeConfig(dlqTopic)
	dlq.GroupID = "order-dlq-consumer"
	dlq.InitialOffset = msq.OffsetResetEarliest

	// The manager comes from the service's broker, so ListenAndServe drains it on shutdown.
	err = msq.NewConsumerManager(mq.Broker()).
		Register(cfg, handleOrder).
		Register(dlq, handleDeadLetter).
		Start()
	if err != nil {
		log.Fatal(err)
	}

	// Blocks until SIGINT/SIGTERM, waits for in-flight handlers, then closes the broker.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleOrder(_ context.Context, msg *msq.Message) (msq.Result, error) {
	order, err := msq.UnpackMessage[OrderCreated](msg)
	if err != nil {
		return msq.Ignore, err // malformed payload: a retry would not fix it
	}
	if order.Amount <= 0 {
		return msq.Nack, errInvalidAmount
	}

	logging.Info("order processed",
		slog.String("id", order.ID),
		slog.String("customer", order.Customer),
		slog.Float64("amount", order.Amount))
	return msq.Ack, nil
}

// handleDeadLetter receives the messages that exhausted their retries; the
// x-gofi-dlq-* headers say where they came from and why they failed.
func handleDeadLetter(_ context.Context, msg *msq.Message) (msq.Result, error) {
	logging.Warn("order dead-lettered",
		slog.String("from", msg.Headers[msq.HeaderDLQOriginalTopic]),
		slog.String("error", msg.Headers[msq.HeaderDLQError]),
		slog.String("attempts", msg.Headers[msq.HeaderDLQAttempts]),
		slog.String("payload", string(msg.Value)))
	return msq.Ack, nil
}

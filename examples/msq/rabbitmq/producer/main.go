// Command producer is a service of its own: it publishes an OrderCreated
// event to RabbitMQ every two seconds until SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/messaging"
	"github.com/gofi-labs/gofi-sdk-go/msq"
	_ "github.com/gofi-labs/gofi-sdk-go/msq/provider/rabbitmq" // enables MESSAGING_PROVIDER=rabbitmq
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

const (
	// exchange is declared by Build; the consumer binds its queues to it.
	exchange = "orders"
	topic    = "orders"
)

// OrderCreated is the event contract. The consumer keeps its own copy:
// the two services share only the JSON shape, never Go code.
type OrderCreated struct {
	ID        string    `json:"id"`
	Customer  string    `json:"customer"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

func main() {
	mq := messaging.New(msq.Config{Exchange: exchange}) // MESSAGING_* → RabbitMQ; Build declares the exchange
	svc, err := gofi.New("order-producer").With(mq).Build()
	if err != nil {
		log.Fatal(err)
	}

	producer, err := mq.Broker().NewProducer()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	publish(ctx, producer)

	// No HTTP server to wait on: Shutdown closes the broker and flushes the logs.
	_ = producer.Close()
	if err := svc.Shutdown(context.Background()); err != nil {
		log.Print(err)
	}
}

func publish(ctx context.Context, producer msq.Producer) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for n := 1; ; n++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		order := OrderCreated{
			ID:        fmt.Sprintf("order-%d", n),
			Customer:  fmt.Sprintf("customer-%d", n%3+1),
			Amount:    float64(n * 10),
			CreatedAt: time.Now(),
		}
		if n%5 == 0 {
			order.Amount = 0 // invalid on purpose: the consumer retries it, then dead-letters it
		}
		if err := send(ctx, producer, order); err != nil {
			logging.Error("publish order", slog.String("id", order.ID), slog.Any("error", err))
			continue
		}
		logging.Info("order published", slog.String("id", order.ID), slog.Float64("amount", order.Amount))
	}
}

func send(ctx context.Context, producer msq.Producer, order OrderCreated) error {
	msg, err := msq.NewMessageWithTopic(topic, order)
	if err != nil {
		return err
	}
	msg.Type = "order.created"
	return producer.SendMessage(ctx, msg)
}

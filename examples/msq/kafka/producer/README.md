# Kafka producer

Publishes an `OrderCreated` event to `orders` every 2 seconds until `Ctrl+C`.
Part of the [Kafka example](../README.md); run the consumer too to see the messages processed.

## Run

```sh
cd examples/msq/kafka
docker compose up -d   # a single-node Kafka on `localhost:9092`
cd producer
go run .
```

`.env` (read automatically outside prod/stage):

```sh
MESSAGING_PROVIDER=kafka
MESSAGING_HOST=localhost
MESSAGING_PORT=9092
```

## How it works

```go
mq := messaging.New(...) // broker from MESSAGING_*
svc, err := gofi.New("order-producer").With(mq).Build()
producer, err := mq.Broker().NewProducer()

msg, err := msq.NewMessageWithTopic("orders", order) // JSON payload
err = producer.SendMessage(ctx, msg)

svc.Shutdown(ctx) // no HTTP server: close the broker and flush logs explicitly
```

- `_ "github.com/joaoprofile/gofi-sdk-go/msq/provider/kafka"` links the provider and enables `MESSAGING_PROVIDER=kafka`.
- Uses the customer as message key, so orders of one customer stay in order.
- Every 5th order has `amount: 0`, to show the consumer's retry and dead-letter path.

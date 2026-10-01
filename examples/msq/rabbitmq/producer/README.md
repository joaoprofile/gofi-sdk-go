# RabbitMQ producer

Publishes an `OrderCreated` event to `orders` every 2 seconds until `Ctrl+C`.
Part of the [RabbitMQ example](../README.md); run the consumer too to see the messages processed.

## Run

```sh
cd examples/msq/rabbitmq
docker compose up -d   # RabbitMQ on `localhost:5672` (UI: http://localhost:15672, `gofi` / `gofi`)
cd producer
go run .
```

`.env` (read automatically outside prod/stage):

```sh
MESSAGING_PROVIDER=rabbitmq
MESSAGING_HOST=localhost
MESSAGING_PORT=5672
MESSAGING_USER=gofi
MESSAGING_PASSWORD=gofi
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

- `_ "github.com/gofi-labs/gofi-sdk-go/msq/provider/rabbitmq"` links the provider and enables `MESSAGING_PROVIDER=rabbitmq`.
- Publishes to the `orders` exchange with routing key `orders` (`Build` declares the exchange).
- Every 5th order has `amount: 0`, to show the consumer's retry and dead-letter path.

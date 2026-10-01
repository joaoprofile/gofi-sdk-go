# RabbitMQ consumer

Processes the `OrderCreated` events from `orders`. Invalid orders are retried
and then dead-lettered to `orders-dlq`, which a second handler logs.
Part of the [RabbitMQ example](../README.md).

## Run

```sh
cd examples/msq/rabbitmq
docker compose up -d   # RabbitMQ on `localhost:5672` (UI: http://localhost:15672, `gofi` / `gofi`)
cd consumer
go run .               # Ctrl+C stops it after the in-flight messages finish
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
mq := messaging.New(...)
svc, err := gofi.New("order-consumer").With(mq).Build()

cfg := msq.DefaultConsumeConfig("orders")
cfg.MaxRetries = 2
cfg.DeadLetterTopic = "orders-dlq"

err = msq.NewConsumerManager(mq.Broker()).
    Register(cfg, handleOrder).
    Register(msq.DefaultConsumeConfig("orders-dlq"), handleDeadLetter).
    Start()

svc.ListenAndServe() // blocks until SIGINT/SIGTERM, then drains the consumers
```

- `msq.NewConsumerManager(mq.Broker())` registers one handler per topic; `ListenAndServe` drains them on shutdown.
- The handler returns `msq.Ack`, `msq.Nack` (retry) or `msq.Ignore` (drop without retry).
- `MaxRetries: 2` + `RetryBackoff` retry a Nack in-process; then `DeadLetterTopic` receives a copy with the `x-gofi-dlq-*` headers.
- Without a `DeadLetterTopic`, a message still nacked on its `MaxDeliveries`-th broker delivery (default 10; `msg.DeliveryCount` tells which one) is rejected and reported as `msq.EventMessageRejected`.
- Declares the durable queues `orders` and `orders-dlq` and binds them to the `orders` exchange.

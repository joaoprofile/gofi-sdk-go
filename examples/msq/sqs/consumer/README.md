# Amazon SQS consumer

Processes the `OrderCreated` events from `orders`. Invalid orders are retried
and then dead-lettered to `orders-dlq`, which a second handler logs.
Part of the [Amazon SQS example](../README.md).

## Run

```sh
cd examples/msq/sqs
docker compose up -d   # LocalStack (SQS) on `localhost:4566`, with the `orders` and `orders-dlq` queues already created
cd consumer
go run .               # Ctrl+C stops it after the in-flight messages finish
```

`.env` (read automatically outside prod/stage):

```sh
MESSAGING_PROVIDER=sqs
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=test
AWS_SECRET_ACCESS_KEY=test
AWS_ENDPOINT_URL_SQS=http://localhost:4566
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
- Long-polls `orders` and `orders-dlq`.

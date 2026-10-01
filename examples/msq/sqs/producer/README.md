# Amazon SQS producer

Publishes an `OrderCreated` event to `orders` every 2 seconds until `Ctrl+C`.
Part of the [Amazon SQS example](../README.md); run the consumer too to see the messages processed.

## Run

```sh
cd examples/msq/sqs
docker compose up -d   # LocalStack (SQS) on `localhost:4566`, with the `orders` and `orders-dlq` queues already created
cd producer
go run .
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
mq := messaging.New(...) // broker from MESSAGING_*
svc, err := gofi.New("order-producer").With(mq).Build()
producer, err := mq.Broker().NewProducer()

msg, err := msq.NewMessageWithTopic("orders", order) // JSON payload
err = producer.SendMessage(ctx, msg)

svc.Shutdown(ctx) // no HTTP server: close the broker and flush logs explicitly
```

- `_ "github.com/joaoprofile/gofi-sdk-go/msq/provider/sqs"` links the provider and enables `MESSAGING_PROVIDER=sqs`.
- Sends to the `orders` queue (resolved by name once, then cached).
- Every 5th order has `amount: 0`, to show the consumer's retry and dead-letter path.

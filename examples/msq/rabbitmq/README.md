# msq — RabbitMQ: producer and consumer

Two **separate services**, each with its own `go.mod`, `.env` and `main.go`.
They share only the broker and the JSON shape of the `OrderCreated` event —
no Go code — just like two services in different repositories.

```
rabbitmq/
├── compose.yaml        # RabbitMQ for local runs
├── producer/           # publishes an OrderCreated every 2 s
└── consumer/           # processes orders; retries, then dead-letters the invalid ones
```

## Run

```sh
cd examples/msq/rabbitmq
docker compose up -d

# terminal 1
cd consumer && go run .

# terminal 2
cd producer && go run .
```

Start the **consumer first**: it declares the `orders` queues and binds them to the
`orders` exchange. Messages published before that are unroutable and dropped.

Stop each service with `Ctrl+C`; `docker compose down` removes the broker.

## What you will see

Every 5th order has `amount: 0` on purpose. The consumer rejects it with
`msq.Nack`, retries it twice and then sends it to `orders-dlq`, where a second
handler logs it:

```
producer  INFO order published      id=order-4 amount=40
consumer  INFO order processed      id=order-4 customer=customer-2 amount=40
producer  INFO order published      id=order-5 amount=0
consumer  WARN order dead-lettered  from=orders error="amount must be greater than zero" attempts=3
```

## RabbitMQ specifics

- `messaging.New(msq.Config{Exchange: "orders"})` — both services use the same exchange; `Build` declares it (direct, durable).
- The consumer declares one durable queue per topic, bound with the topic as routing key.

## Moving to production

Only the environment changes — the code stays the same. Set `APP_ENVIRONMENT=prod`
(the `.env` file is then ignored) and provide the `MESSAGING_*` variables from the
platform; any value may be a secret reference (`secret://awssm/...`).
Switching broker means changing the blank import and `MESSAGING_PROVIDER`.

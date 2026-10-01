# gofi examples

Small, runnable projects. Each one is an **independent Go module** (its own
`go.mod`, `.env` and `README.md`), so it can be copied out, built and run on its own.

| Project | Kind | What it shows |
|---------|------|---------------|
| [netx/api](netx/api) | HTTP API | Handlers, public/private routes, global and auth middlewares |
| [sqln/search](sqln/search) | Job | PostgreSQL + migrations, `criteria` filters, pagination, streaming |
| [sqln/filter-api](sqln/filter-api) | HTTP API | Dynamic filters sent by the client, allowlisted by `sqln.FilterMapping` |
| [msq/rabbitmq](msq/rabbitmq) | Producer + consumer | Two services on RabbitMQ, retries and dead-letter queue |
| [msq/kafka](msq/kafka) | Producer + consumer | Two services on Kafka, message keys and consumer groups |
| [msq/sqs](msq/sqs) | Producer + consumer | Two services on Amazon SQS (LocalStack) |
| [obs](obs) | HTTP API + worker + job | Traces, metrics and logs to Grafana (Prometheus, Tempo, Loki): handlers, HTTP calls, queues, jobs |
| [iam/login](iam/login) | HTTP API | Login without a database: JWT bearer tokens or session cookie with the token kept on the backend, refresh, logout, RBAC |

## Running any example

```sh
cd examples/<project>
docker compose up -d   # only when the project has a compose.yaml
go run .
```

Requirements: Go 1.26+ and Docker (for PostgreSQL, RabbitMQ, Kafka, LocalStack or the Grafana stack).

- Configuration comes from the `.env` next to `main.go`, read automatically
  outside `prod`/`stage`. Variables already set in the shell win over the file.
- Each `go.mod` points at the SDK in this repository through `replace`
  directives. In your own project, drop the `replace` block and `go get` the
  modules instead.
- The msq examples have `producer/` and `consumer/` as two separate modules:
  run each one in its own terminal.

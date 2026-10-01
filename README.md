# gofi

Modular SDK for Go that provides the fundamental building blocks for building microservices: database, cache, messaging, HTTP (server, client and request signing), object storage, secrets, e-mail, observability, authentication, and core utilities.

The project is organized as a **multi-module monorepo**: each library, provider and cloud integration is an independent Go module with its own `go.mod`. Consumers import only what they need, and a binary links a vendor SDK (Kafka, AWS, OCI, ...) only when it imports the module that wraps it.

---

## Quick start

Minimal snippets to get a feel for the SDK. Complete, runnable programs live in [examples/](examples).

### A service with `gofi.New()`

The orchestrator reads the environment, starts the components you declare and closes them on SIGINT/SIGTERM. Each component (database, cache, session, messaging, observability, IAM, HTTP server) is its own package, so the binary links only the ones you import.

```bash
# .env
DATABASE_DRIVER=postgres
DATABASE_HOST=localhost
DATABASE_NAME=catalog
DATABASE_USER=catalog
DATABASE_PASSWORD=secret
```

```go
package main

import (
    "log"

    "github.com/gofi-labs/gofi-sdk-go/gofi"
    "github.com/gofi-labs/gofi-sdk-go/gofi/component/database"
    "github.com/gofi-labs/gofi-sdk-go/gofi/component/httpserver"
    _ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/postgres" // DATABASE_DRIVER=postgres
)

func main() {
    svc, err := gofi.New("catalog-api").
        With(
            database.New(), // DATABASE_* → global sqln connection
            httpserver.New(":8080").Handlers(&HelloHandler{}),
        ).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    if err := svc.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
```

### An HTTP handler with `netx`

A handler is any type that implements `netx.RouterHandler` — it declares its own routes.

```go
import (
    "net/http"

    "github.com/gofi-labs/gofi-sdk-go/netx"
)

type HelloHandler struct{}

func (h *HelloHandler) Handlers() []*netx.Route {
    return netx.PublicRoutes("/hello",
        netx.GET("/{name}").To(h.hello),
    )
}

func (h *HelloHandler) hello(w http.ResponseWriter, r *http.Request) {
    name := netx.GetPathParam("name", r)
    netx.Response(w, http.StatusOK, map[string]string{"message": "hello, " + name})
}
```

### A query with `sqln`

Build the query with `criteria` and let the generic manager map rows through the `db` tags.

```go
import (
    "context"

    "github.com/gofi-labs/gofi-sdk-go/sqln"
    "github.com/gofi-labs/gofi-sdk-go/sqln/criteria"
)

type Product struct {
    ID    int64   `db:"id"`
    Name  string  `db:"name"`
    Price float64 `db:"price"`
}

func FindCheaperThan(ctx context.Context, max float64) ([]Product, error) {
    q := criteria.From("products", "p").
        Select("p.id", "p.name", "p.price").
        Where(criteria.Lte("p.price", max))

    return sqln.FindFromCriteria[Product](ctx, q).List()
}
```

### More examples

Each example is a standalone Go module with its own `README.md`: `cd` into it, start the infrastructure with `docker compose up -d` when there is a `compose.yaml`, and `go run .`.

| Example | What it shows |
|---------|---------------|
| [examples/netx/api](examples/netx/api) | `netx` server through the `httpserver` component: several handlers, public/private routes, global and auth middlewares |
| [examples/sqln/search](examples/sqln/search) | Job (no HTTP) on PostgreSQL: migrations, `criteria` filters, join, pagination, single row and streaming |
| [examples/sqln/filter-api](examples/sqln/filter-api) | HTTP API with client-driven dynamic filters, allowlisted by `sqln.FilterMapping` |
| [examples/msq/rabbitmq](examples/msq/rabbitmq) | Producer and consumer as separate services on RabbitMQ, with retries and dead-letter queue |
| [examples/msq/kafka](examples/msq/kafka) | Same pair on Kafka: message keys, consumer groups |
| [examples/msq/sqs](examples/msq/sqs) | Same pair on Amazon SQS (LocalStack locally, AWS default credential chain) |
| [examples/obs](examples/obs) | Traces, metrics and logs to a local Grafana stack (collector, Prometheus, Tempo, Loki): instrumented handlers, HTTP calls, queue propagation, jobs as `gofi.Runner`, provisioned dashboard |
| [examples/iam/login](examples/iam/login) | Login without a database: bearer JWT with refresh rotation, or session cookie with the tokens kept on the backend; logout, logout-all and RBAC |


---

## Design principles

1. **Predictable lifecycle.** `gofi.New` and `With` only declare what the service needs. `Build()` starts the components in a fixed order (by stage) and returns **every** error; if a step fails, it undoes what was already opened. `ListenAndServe()` handles SIGINT/SIGTERM, drains and closes resources in reverse order. No library calls `log.Fatal` or `os.Exit`.
2. **Modular, cloud-native providers.** Each broker, bucket backend, secret manager, signer and cloud identity is its own module, enabled by import. Identity comes from each cloud's default chain (IRSA / EKS Pod Identity, Workload Identity, instance/resource principals) — no static keys required.
3. **Secure by default.** Dynamic filters require an allowlist, the default timezone is UTC, `.env` is disabled in production, secrets can come from `secret://` references, HTTP client retries only idempotent methods, and passwords are hashed with Argon2id.
4. **Libraries don't know about the environment.** Leaf libraries take explicit, typed `Config` structs. Only the `gofi` module (`gofi/config`, `gofi/config/core` and each `gofi/component/*`) maps `base/environment` into those structs.
5. **Pay only for what you import.** The `gofi` package knows only the `Component` contract. A service without `gofi/component/observability` links no gRPC or OpenTelemetry SDK; without `gofi/component/database`, no SQL driver; `make size-check` guards this.

---

## Architecture

### Layers

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Orchestrator — github.com/gofi-labs/gofi-sdk-go                         │
│    gofi.New("svc").With(database.New(), httpserver.New(":8080")).Build() │
│    component/{database,cache,session,messaging,observability,iam,        │
│               httpserver}                                                │
│    config/, config/core  ← env → typed Config bridge                     │
└───────────────┬──────────────────────────────────────────────────────────┘
                │ imports
┌───────────────▼──────────────────────────────────────────────────────────┐
│  Libraries (explicit Config, no env access)                              │
│    sqln     msq     netx     iam     obs                                 │
└───────────────┬──────────────────────────────────────────────────────────┘
                │ imports
┌───────────────▼──────────────────────────────────────────────────────────┐
│  Foundation — base                                                       │
│    environment · errs · bucket · secrets · mail · session · cronjob ...  │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│  Provider modules (opt-in, carry the vendor SDKs)                        │
│    msq/provider/{kafka,rabbitmq,sqs,oci,redis,nats}                      │
│    base/bucket/{s3,oci}   base/secrets/{awssm,ocivault}                  │
│    netx/awssign           sqln/rdsauth                                   │
└───────────────┬──────────────────────────────────────────────────────────┘
                │ credentials
┌───────────────▼──────────────────────────────────────────────────────────┐
│  Cloud identity — base/cloud/{aws,oci}   (vendor SDK only)               │
└──────────────────────────────────────────────────────────────────────────┘
```

### Dependency hierarchy between modules

```
gofi                        → base, obs/logging (gofi/config/core)
gofi/component/*            → gofi + the library it starts (sqln, msq, netx, obs)
gofi/config                 → base, iam, obs/logging
obs                         → base
sqln                        → base, obs
msq                         → obs (base transitively)
netx                        → base, obs
iam                         → (no internal dependencies)
base                        → (no internal dependencies)

msq/provider/{kafka,rabbitmq,redis,nats} → msq, obs
msq/provider/sqs            → msq, obs, base/cloud/aws
msq/provider/oci            → msq, obs, base/cloud/oci
base/bucket/s3              → base, base/cloud/aws
base/bucket/oci             → base, base/cloud/oci
base/secrets/awssm          → base, base/cloud/aws
base/secrets/ocivault       → base, base/cloud/oci
netx/awssign                → netx, base/cloud/aws
sqln/rdsauth                → vendor SDK only
base/cloud/{aws,oci}        → vendor SDK only
examples/*                  → gofi modules via replace (each one its own module)
```

`base` is the foundation of the stack and `obs` sits right above it. There is never a circular dependency, and the `gofi` module never imports a provider: providers register themselves when the application blank-imports them.

### Module catalogue

| Module                                   | Kind          | Responsibility                                      |
| ---------------------------------------- | ------------- | --------------------------------------------------- |
| `.../gofi`                               | orchestrator  | `Builder`/`Service`, components, `config` adapters  |
| `.../base`                               | foundation    | environment, errors, bucket API, secrets, mail, ... |
| `.../obs`                                | library       | OpenTelemetry traces/metrics, `slog` logging        |
| `.../sqln`                               | library       | SQL access, pagination, filters, cache, migrations  |
| `.../msq`                                | library       | Broker abstraction and consumer pipeline            |
| `.../netx`                               | library       | HTTP server, HTTP client, `Signature` interface     |
| `.../iam`                                | library       | JWT, sessions, RBAC, identity providers             |
| `.../msq/provider/kafka`                 | provider      | Apache Kafka (CloudEvents binary)                   |
| `.../msq/provider/rabbitmq`              | provider      | RabbitMQ                                            |
| `.../msq/provider/sqs`                   | provider      | AWS SQS (standard and FIFO)                         |
| `.../msq/provider/oci`                   | provider      | Oracle Cloud Queue                                  |
| `.../msq/provider/redis`                 | provider      | Redis Pub/Sub or Streams                            |
| `.../msq/provider/nats`                  | provider      | NATS JetStream                                      |
| `.../base/bucket/s3`                     | provider      | AWS S3, MinIO, R2 and other S3-compatible stores    |
| `.../base/bucket/oci`                    | provider      | OCI Object Storage                                  |
| `.../base/secrets/awssm`                 | provider      | AWS Secrets Manager (`secret://awssm/...`)          |
| `.../base/secrets/ocivault`              | provider      | OCI Vault (`secret://ocivault/...`)                 |
| `.../netx/awssign`                       | provider      | AWS SigV4 request signer                            |
| `.../sqln/rdsauth`                       | provider      | RDS / Aurora IAM authentication tokens              |
| `.../base/cloud/aws`                     | cloud         | AWS credential chain                                |
| `.../base/cloud/oci`                     | cloud         | OCI authentication modes                            |
| `.../examples/...`                       | examples      | Runnable samples, one module each (not published)   |

---

## Modules

### `gofi` — Main orchestrator

**Path:** `github.com/gofi-labs/gofi-sdk-go/gofi` (components under `.../gofi/component/<name>`)

Entry point of the SDK. Exposes the `Builder` and `Service` interfaces, the `Component` contract and `New(serviceName)`. `With(...)` declares components; `Build()` loads the environment, sets up logging and starts them by stage (observability → database → cache → session → messaging → IAM → HTTP server), whatever the order of declaration.

| Component package                  | Env-driven (`Build` opens and closes it) | Injected (the caller owns it) | Handle                     |
| ---------------------------------- | ---------------------------------------- | ----------------------------- | -------------------------- |
| `component/database`               | `database.New()` + `import _ sqln/driver/<name>` | `database.FromDB(db)` | `DB()`, `ReadDB()`         |
| `component/cache`                  | `cache.New()`                            | `cache.FromClient(rdb)`       | `Client()`                 |
| `component/session`                | `session.New(cfg...)`                    | —                             | `session.Instance()` (base) |
| `component/messaging`              | `messaging.New(msq.Config...)` + `import _ msq/provider/<name>` | `messaging.FromBroker(b)` | `Broker()` |
| `component/observability`          | `observability.New()` (OTEL_*)           | `observability.FromTelemetry(t)` | `Telemetry()`           |
| `component/iam`                    | `iam.New(iam.Config...)` (JWT_*, `*_TOKEN_TTL`, CACHE_* for Redis sessions) | `iam.FromService(svc)` | `Service()` |
| `component/httpserver`             | `httpserver.New(port, cfg...)`           | `httpserver.FromServer(s)`    | `Server()`; `.Handlers/.Use/.UseAuth` |

```go
db := database.New()
mq := messaging.New() // MESSAGING_PROVIDER=kafka + import _ ".../msq/provider/kafka"

svc, err := gofi.New("my-service").
    With(
        db,
        cache.New(),
        mq,
        httpserver.New(":8080", &netx.WSConfig{Health: &netx.HealthConfig{}}).
            Handlers(handlers...),
    ).
    Build() // returns every configuration error at startup
if err != nil {
    log.Fatal(err)
}
producer, err := mq.Broker().NewProducer()

// Blocks until SIGINT/SIGTERM (or svc.Shutdown), drains HTTP and closes
// consumers, cache, database and telemetry in reverse order.
if err := svc.ListenAndServe(); err != nil {
    log.Fatal(err)
}
```

A custom resource is a type with `Name()`, `Stage()` and `Start(ctx, *gofi.Runtime)`; it registers what it opens with `rt.OnClose` and its readiness checks with `rt.AddHealthCheck`. A component that also implements `Run()`/`Stop(ctx)` (`gofi.Runner`) is served by `ListenAndServe`.

#### `config` — Environment composition layer

**Path:** `github.com/gofi-labs/gofi-sdk-go/gofi/config` (part of the `gofi` module)

Maps `base/environment` into each library's typed `Config`. Every adapter takes an `*environment.Environment` explicitly, so it is testable without the process-wide singleton. Applications that don't use gofi's environment loader can ignore it and build each `Config` themselves. The mappings of the resources gofi starts live with their components, and the process-wide settings in `config/core` (imported by `Build` without the rest of `config`).

| Adapter                                  | Produces                                     |
| ---------------------------------------- | -------------------------------------------- |
| `database.ConfigFromEnv(env)`            | `connection.Config` (sqln)                   |
| `cache.Configure(env)`                   | configures the sqln query cache              |
| `messaging.ConfigFromEnv(env)`           | `msq.ProviderConfig` → `msq.Open(ctx, cfg)`  |
| `config.Bucket(env)`                     | `bucket.Config` → `bucket.Open(ctx, cfg)`    |
| `config.IAM(env)`                        | `iamconfig.DefaultConfig` → `iam.NewDefault` |
| `config.Mail(env)` / `NewMailer(env)`    | `mail.Config` / `mail.Mailer`                |
| `observability.ConfigFromEnv(env)`       | `obs.TeleConfig`                             |
| `config.Logging(env, svc)` / `InitLogging` | `logging.Config`                           |
| `config.Timezone(env)` / `SetTimezone`   | `timezone.Config`                            |
| `config.Debug(env)` / `StartDebug`       | `debug.Config`                               |
| `config.ApplyTLS(env)`                   | process-wide TLS settings                    |

---

### `base` — Foundation

**Path:** `github.com/gofi-labs/gofi-sdk-go/base`

Infrastructure utilities and services used by all other modules. Has no internal dependencies within gofi.

| Sub-package   | Responsibility                                                                 |
| ------------- | ------------------------------------------------------------------------------ |
| `environment` | Configuration loader via environment variables (`.env` off in prod/stage)      |
| `errs`        | `AppError` with `ErrorKind` classification, JSON-friendly                      |
| `secrets`     | `secret://<provider>/<name>[#key]` resolution; `env`, `file` built in          |
| `bucket`      | Object storage API; `mem`, `file` built in; `s3`, `oci` modules; `buckettest` contract |
| `cloud`       | Cloud identity shared by the integrations; `aws`, `oci` modules                |
| `mail`        | SMTP e-mail: HTML + text, attachments, templates, TLS/STARTTLS, retry, pooling |
| `session`     | Session management with Redis support                                          |
| `cronjob`     | Worker pool and scheduler; `Locker` runs each slot on one replica              |
| `timezone`    | Process-wide `time.Local` (UTC by default, embedded IANA database)             |
| `observer`    | Lifecycle hooks registry and global WaitGroup                                  |
| `validator`   | Struct validation with custom tag support                                      |
| `common`      | Utilities: strings, reflect, converters                                        |
| `debug`       | Diagnostic HTTP server (non-prod environments only)                            |

```go
import "github.com/gofi-labs/gofi-sdk-go/base/environment"

env := environment.Instance()
fmt.Println(env.AppName, env.AppEnvironment)
```

---

### `obs` — Observability

**Path:** `github.com/gofi-labs/gofi-sdk-go/obs`

Integration with OpenTelemetry: traces, metrics, and structured logs via `slog`. The package is split by weight: `obs/logging` (console `slog` logger) and `obs/metrics` (instrument helpers, database pool stats) link no exporter; `obs` itself holds `Init`, which exports traces, metrics **and logs** over one OTLP/gRPC connection, attaching the log exporter to the global logger with `logging.Attach`.

| Export                                    | Description                               |
| ----------------------------------------- | ----------------------------------------- |
| `obs.Init(ctx, TeleConfig)`               | Traces, metrics and logs over OTLP/gRPC   |
| `obs/metrics.Meter()`                     | Returns the global `metric.Meter`         |
| `obs/metrics.NewFloat64Histogram(...)`    | Creates a float64 histogram (and friends) |
| `obs/metrics.ObserveDBStats(pool, db)`    | `sql.DB` pool gauges                      |
| `obs/logging.Info/Error/Warn/Debug/Fatal` | Global structured logging                 |
| `obs/logging.Attach(handler, flush)`      | Tees the global logger to another handler, at the configured level |

```go
import (
    "github.com/gofi-labs/gofi-sdk-go/obs"
    "github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

tele, err := obs.Init(ctx, obs.TeleConfig{
    ServiceName:   "my-service",
    CollectorAddr: "otel-collector:4317",
})

logging.Info("service started", slog.String("port", ":8080"))
```

---

### `sqln` — Database and cache

**Path:** `github.com/gofi-labs/gofi-sdk-go/sqln`

Data access layer for SQL with support for PostgreSQL (pgx/v5), MySQL, SQL Server, and Oracle. Includes pagination, allowlisted dynamic filters, query caching (Redis or in-memory), migrations, a read replica (`DATABASE_READ_HOST`), per-connection passwords for IAM tokens (`sqln/rdsauth`) and transaction retries on serialization conflicts.

| Sub-package   | Responsibility                                     |
| ------------- | -------------------------------------------------- |
| `connection`  | SQL connection pool with configuration and drivers |
| `driver`      | Dialects: `postgres`, `mysql`, `sqlserver`, `oracle` |
| `query`       | Read queries on the global connection              |
| `statement`   | Write statements and single-row queries            |
| `mapping`     | Struct ↔ row mapping using `` `db:"col"` `` tag    |
| `pagination`  | `PageRequest`, `Sort`, `Page[T]`                   |
| `cache`       | Query result caching (Redis or in-memory)          |
| `criteria`    | Chainable query builder by predicates              |
| `filter`      | Dynamic filters from requests (allowlist mapping)  |
| `transaction` | Transaction management with context propagation    |
| `migrate`     | Running migrations via filesystem                  |

```go
import "github.com/gofi-labs/gofi-sdk-go/sqln"

type User struct {
    ID   int64  `db:"id"`
    Name string `db:"name"`
}

user, err := sqln.Find[User](ctx, "SELECT id, name FROM users WHERE id = $1", id).UniqueResult()

// Dynamic filters from a request: only names in the mapping are accepted,
// each translated to its column; placeholders continue after the base args.
mapping := sqln.FilterMapping{
    "status":  {Column: "o.status", Ops: sqln.Equality},
    "created": {Column: "o.created_at", Ops: sqln.Range, Sortable: true},
}
q, err := sqln.BuildQuery("SELECT ... FROM orders o WHERE o.tenant_id = $1", []any{tenantID}, filters, mapping, nil)
page, err := sqln.NewPageRequestFilter(filters, mapping)
list, err := sqln.FindWithFilter[Order](ctx, q).WithPage(page).PagedList()

// Stream large results without building a slice.
for u, err := range sqln.Find[User](ctx, "SELECT id, name FROM users").All() {
    if err != nil { return err }
    process(u)
}

// Retry the whole transaction on serialization failures and deadlocks.
tx := transaction.New(transaction.Options{Isolation: sql.LevelSerializable, MaxRetries: 3})
err = tx.Execute(ctx, func(ctx context.Context) error {
    q := connection.QuerierFrom(ctx, db) // the transaction carried by ctx
    _, err := q.ExecContext(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", amount, id)
    return err
})
```

**RDS / Aurora IAM auth** (`sqln/rdsauth`): databases accept the pod's AWS identity instead of a static password. Tokens last 15 minutes and are signed for every new connection (requires TLS).

```go
awsCfg, _ := cloudaws.Load(ctx, cloudaws.Config{})
cfg.Password = rdsauth.Password(awsCfg, "db.xxxx.rds.amazonaws.com:5432", "app")
```

---

### `msq` — Messaging

**Path:** `github.com/gofi-labs/gofi-sdk-go/msq`

Message broker abstraction with support for multiple providers. The `Broker` interface is uniform — switching from Kafka to RabbitMQ only requires changing the provider.

| Sub-package | Responsibility                                          |
| ----------- | ------------------------------------------------------- |
| `types`     | Message, headers and events                             |
| `port`      | `Broker`, producer and consumer interfaces              |
| `core`      | Consumer pipeline (retry, DLQ, drain, tracing)          |
| `worker`    | Concurrent handler execution                            |
| `msqtest`   | Contract every provider must satisfy                    |

Each provider is its own module and registers itself on import. With the
builder, blank-import it and set `MESSAGING_PROVIDER=<p>`; standalone, call
`msq.Open(ctx, messaging.ConfigFromEnv(env))` or the provider's `New` directly.

| Provider           | Provider module (`msq/provider/…`) |
| ------------------ | ---------------------------------- |
| Apache Kafka       | `kafka` (CloudEvents binary)       |
| RabbitMQ           | `rabbitmq`                         |
| AWS SQS            | `sqs` (AWS default chain, IRSA)    |
| Oracle Cloud Queue | `oci` (API key or principals)      |
| Redis              | `redis` (Pub/Sub or Streams)       |
| NATS JetStream     | `nats`                             |

```go
import _ "github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka" // with gofi.New(...).With(messaging.New())
```

Every consumer created through `msq.New` runs the same pipeline, whatever the
provider: handler panics become a Nack, `MaxRetries`/`RetryBackoff` retry with
jittered backoff, `DeadLetterTopic` receives a copy (with `x-gofi-dlq-*`
headers) once retries are exhausted, in-flight handlers finish during shutdown
(`HandlerTimeout` bounds them), and W3C trace context flows through message
headers with `send`/`process` spans and `messaging.*` metrics.

```go
import (
    "github.com/gofi-labs/gofi-sdk-go/msq"
    "github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka"
)

broker, _ := kafka.New(kafka.Config{Brokers: []string{"localhost:9092"}})
svc, _    := msq.New(msq.Config{Broker: broker})

producer, _ := svc.NewProducer()
msg, err := msq.NewMessageWithTopic("orders", order) // encoding errors are returned
if err != nil {
    return err
}
producer.SendMessage(ctx, msg)
```

---

### `netx` — HTTP

**Path:** `github.com/gofi-labs/gofi-sdk-go/netx`

HTTP server and client based on `go-chi`. Includes ready-to-use middlewares, health probes and a pluggable request signer.

| Export                          | Description                                          |
| ------------------------------- | ---------------------------------------------------- |
| `netx.NewServer(cfg)`           | Creates HTTP server with chi                         |
| `netx.NewClient(cfg)`           | Creates HTTP client with retry and rate limit        |
| `netx.NewRequest[T](...)`       | Typed request; `SetHeader`, `SetBody`, `SetSignature`, `Execute` |
| `netx.Signature`                | Request signer interface (see below)                 |
| `netx.CORSMiddleware(cfg)`      | Configurable CORS middleware                         |
| `netx.NewRedisRateLimiter(...)` | Rate limiter via Redis                               |
| `netx.LoggingMiddleware`        | Structured request logging                           |
| `netx.SecurityHeaders`          | Security headers (CSP, HSTS, etc.)                   |

```go
import "github.com/gofi-labs/gofi-sdk-go/netx"

server := netx.NewServer(&netx.WSConfig{ServerPort: ":8080"})
server.Use(netx.LoggingMiddleware(), netx.SecurityHeaders)
server.AddHandlers(myRouter)
if err := server.ListenAndServe(); err != nil { // stops on SIGINT/SIGTERM or server.Shutdown
    log.Fatal(err)
}
```

Every request gets an OpenTelemetry server span named after its route
(`GET /orders/{id}`) and `http.server.*` metrics labeled with `http.route`;
health probes are not traced.

Opt-in `WSConfig.H2C` serves HTTP/2 without TLS (service meshes, HTTP/2 load
balancers). Cross-origin protection (CSRF) is on for every route: unsafe
cross-site requests are rejected unless they come from an origin the route's
CORS policy allows; same-origin requests and clients that send neither
`Origin` nor `Sec-Fetch-Site` (services, curl) pass. Keep `AllowedOrigins`
exact and leave `WSConfig.DisableCrossOriginProtection` off on
cookie-authenticated routes.

The HTTP client retries only idempotent methods (5xx, network errors and
`429`, honoring `Retry-After`); set `DisableRetryOn429` when an external rate
limiter owns the pacing.

#### Request signing

Signing is an interface in `netx`, and each concrete signer lives in its own
module so the core never links a cloud SDK:

```go
// netx.Signature — body is the exact payload sent.
type Signature interface {
    Sign(originalRequest *http.Request, body []byte) (*http.Request, error)
}
```

The client calls `Sign` on **every attempt**, after building the request and
before applying the headers set with `SetHeader`, so each retry carries a fresh
signature and timestamp. A signing error aborts the request (`signature request
error: ...`).

**AWS SigV4** (`netx/awssign`) — API Gateway IAM auth, OpenSearch, Lambda
function URLs, ... Credentials resolve through `base/cloud/aws`, so IRSA / EKS
Pod Identity work without static keys. The signing name defaults to
`execute-api` (`awssign.ServiceExecuteAPI`).

```go
import (
    "github.com/gofi-labs/gofi-sdk-go/netx"
    "github.com/gofi-labs/gofi-sdk-go/netx/awssign"
)

signer, err := awssign.New(ctx, awssign.Config{}) // default chain, execute-api
// signer, err := awssign.New(ctx, awssign.Config{Service: "es"})       // OpenSearch
// signer, err := awssign.NewWithConfig(existingAWSConfig, "execute-api") // reuse an aws.Config

client, _ := netx.NewClient(&netx.HttpClientConfig{BaseURL: "https://abc.execute-api.us-east-1.amazonaws.com"})
req := netx.NewRequest[Order](ctx, client, http.MethodPost, "/orders")
req.SetBody(order)
req.SetSignature(signer)
created, err := req.Execute()
```

**Custom signers** only need to implement `Sign` — e.g. an HMAC webhook signature:

```go
type hmacSigner struct{ key []byte }

func (s hmacSigner) Sign(req *http.Request, body []byte) (*http.Request, error) {
    mac := hmac.New(sha256.New, s.key)
    mac.Write(body)
    req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
    return req, nil
}
```

---

### `base/cloud` — Cloud identity

`base/cloud/aws` and `base/cloud/oci` resolve credentials once for every
integration (bucket, secrets, queue, request signing, RDS).

| Module           | Credentials                                                                |
| ---------------- | -------------------------------------------------------------------------- |
| `base/cloud/aws` | AWS default chain: env, profile, IRSA / EKS Pod Identity, ECS, EC2 roles   |
| `base/cloud/oci` | `api_key`, `instance_principal`, `resource_principal`, `workload_identity` |

| Consumer               | Uses             |
| ---------------------- | ---------------- |
| `base/bucket/s3`       | `base/cloud/aws` |
| `base/secrets/awssm`   | `base/cloud/aws` |
| `msq/provider/sqs`     | `base/cloud/aws` |
| `netx/awssign`         | `base/cloud/aws` |
| `sqln/rdsauth`         | an `aws.Config` (e.g. from `base/cloud/aws`) |
| `base/bucket/oci`      | `base/cloud/oci` |
| `base/secrets/ocivault`| `base/cloud/oci` |
| `msq/provider/oci`     | `base/cloud/oci` |

Object storage: `base/bucket` defines the API; blank-import `base/bucket/s3`
(AWS S3, MinIO, R2, …) or `base/bucket/oci` and call
`bucket.Open(ctx, config.Bucket(env))`.

---

### `iam` — Identity and authentication

**Path:** `github.com/gofi-labs/gofi-sdk-go/iam`

Identity, authentication, and authorization service. Supports JWT, session, RBAC, and multiple Identity Providers. Has no internal dependencies: environment mapping happens in the root `config.IAM(env)`.

| Sub-package          | Responsibility                                                    |
| -------------------- | ----------------------------------------------------------------- |
| `core`               | `IAMService`: authentication, session, and RBAC                   |
| `port`               | Interfaces: `AuthPort`, `IDPAuthPort`, `SessionPort`, `TokenPort` |
| `types`              | Credentials, tokens, users and sessions                           |
| `config`             | `DefaultConfig` consumed by `iam.NewDefault` (ports included)     |
| `provider/jwt`       | JWT (HS256/RS256/ES256), `kid` and key rotation                   |
| `provider/redis`     | Session persistence in Redis                                      |
| `provider/memory`    | In-memory session store                                           |
| `provider/rbac/roles`| Role-based access control                                         |
| `provider/password`  | Argon2id hashing; verifies bcrypt, `NeedsRehash` for migration    |
| `provider/bcrypt`    | bcrypt hashing                                                    |
| `provider/google`    | Google OAuth                                                      |
| `provider/microsoft` | Microsoft OAuth                                                   |
| `provider/oidc`      | Generic OIDC provider                                             |
| `middleware`         | Authentication middleware for HTTP and gRPC                       |

```go
import "github.com/gofi-labs/gofi-sdk-go/iam"

// users implements port.UserPort and port.TenantPort over your storage.
svc, err := iam.NewDefault(iam.DefaultConfig{
    JWTSecret: os.Getenv("JWT_SECRET"), // 32+ bytes; sessions in memory without RedisAddr
    User:      users,
    Tenant:    users,
    RBAC:      roles.NewRBACProvider(roles.Config{Permissions: perms}),
})
if err != nil {
    return err
}

res, err := svc.Authenticate(ctx, port.AuthInput{Email: email, Password: password})
session, err := svc.SelectTenant(ctx, port.SelectTenantInput{
    UserID: res.UserID, TenantID: res.Tenants[0].Tenant.ID, Ticket: res.Ticket,
})
claims, err := svc.ValidateToken(ctx, session.AccessToken) // on every request
```

Without `User` and `Tenant` the service still validates tokens and revokes
sessions; the login operations return `core.ErrLoginPortsRequired`. With
gofi, the `iam` component builds it from the environment (`JWT_SECRET`,
`ACCESS_TOKEN_TTL`, Redis sessions with `CACHE_TYPE=redis`) and takes the
ports in `iam.Config`; see [examples/iam/login](examples/iam/login).

---

## Installation

### Full usage via orchestrator

```bash
go get github.com/gofi-labs/gofi-sdk-go/gofi
```

### Per-module usage (only what you need)

```bash
go get github.com/gofi-labs/gofi-sdk-go/netx    # HTTP only
go get github.com/gofi-labs/gofi-sdk-go/sqln    # database only
go get github.com/gofi-labs/gofi-sdk-go/msq     # messaging only
go get github.com/gofi-labs/gofi-sdk-go/obs     # observability only
go get github.com/gofi-labs/gofi-sdk-go/iam     # authentication only
go get github.com/gofi-labs/gofi-sdk-go/base    # base utilities only

# providers (only the ones you use)
go get github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka     # or rabbitmq, sqs, oci, redis, nats
go get github.com/gofi-labs/gofi-sdk-go/base/bucket/s3         # or base/bucket/oci
go get github.com/gofi-labs/gofi-sdk-go/base/secrets/awssm     # or base/secrets/ocivault
go get github.com/gofi-labs/gofi-sdk-go/netx/awssign           # AWS SigV4 signer
go get github.com/gofi-labs/gofi-sdk-go/sqln/rdsauth           # RDS IAM auth
```

Each module is versioned with its own SemVer tag (`<module dir>/vX.Y.Z`, e.g. `gofi/v0.8.1`, `netx/awssign/v0.8.1`). The repository root is not a module: until v0.7.x the orchestrator was the root module (`github.com/gofi-labs/gofi-sdk-go`); from v0.8.1 it is `.../gofi`.

---

## Configuration via environment variables

The `base/environment` module loads configuration from environment variables at startup (a `.env` file is read only outside `prod`/`stage`); the root `config` package maps it into each library's `Config`. Any value may be a secret reference, `secret://<provider>/<name>[#json-key]`, resolved once at startup: `env` and `file` are built in, `awssm` (AWS Secrets Manager) and `ocivault` (OCI Vault) are blank-imported modules under `base/secrets`. `X_FILE` variants (e.g. `DATABASE_PASSWORD_FILE`) read the value from a file.

| Prefix / variables     | Module      | Examples                                                   |
| ---------------------- | ----------- | ---------------------------------------------------------- |
| `APP_*`                | gofi        | `APP_NAME`, `APP_VERSION`, `APP_ENVIRONMENT`, `APP_TENANT` |
| `DATABASE_*`           | sqln        | `DATABASE_DRIVER`, `DATABASE_HOST`, `DATABASE_READ_HOST`   |
| `CACHE_*`              | sqln/base   | `CACHE_TYPE`, `CACHE_URI`, `CACHE_USE_TLS`                 |
| `MESSAGING_*`          | msq         | `MESSAGING_PROVIDER`, `MESSAGING_HOST`, `MESSAGING_OCI_*`  |
| `BUCKET_*`             | bucket      | `BUCKET_PROVIDER`, `BUCKET_NAME`, `BUCKET_S3_*`, `BUCKET_OCI_*` |
| `MAIL_*`               | mail        | `MAIL_HOST`, `MAIL_PORT`, `MAIL_ENCRYPTION`, `MAIL_AUTH`   |
| `AWS_*`                | cloud/aws   | standard AWS chain (`AWS_REGION`, IRSA)                    |
| `JWT_*`, `OAUTH_*`, `*_TOKEN_TTL` | iam | `JWT_SECRET`, `JWT_KEY_ID`, `ACCESS_TOKEN_TTL`       |
| `OTEL_*`, `LOG_*`      | obs         | `OTEL_EXPORTER_OTLP_ENDPOINT`, `LOG_LEVEL`                 |
| `SERVICE_DEBUG*`       | debug       | `SERVICE_DEBUG`, `SERVICE_DEBUG_ADDR`                      |
| `TIMEZONE`             | timezone    | `America/Sao_Paulo` (empty → UTC)                          |
| `TLS_*`, `ALLOWED_ORIGINS` | netx    | `TLS_INSECURE_SKIP_VERIFY`, `ALLOWED_ORIGINS`              |
| any variable           | secrets     | `DATABASE_PASSWORD=secret://awssm/prod/db#password`        |

### Production transport guard

With `APP_ENVIRONMENT=prod` or `stage`, `Build` refuses to start when the database, cache, broker,
OTLP exporter or object storage is reached without verified TLS, or when `TLS_INSECURE_SKIP_VERIFY`
is set. `GOFI_ALLOW_INSECURE_TRANSPORT` downgrades the refusal to a logged warning for the listed
resources; treat each use as a documented exception. An HTTP server without TLS only logs a warning,
unless `HTTP_REQUIRE_TLS=true`. See [SECURITY.md](SECURITY.md) for the full hardening guidance.

### Security and transport variables

| Variable | Effect |
| -------- | ------ |
| `APP_ENVIRONMENT` | `dev`, `test`, `stage` or `prod`; any other value fails `Build` |
| `GOFI_ALLOW_INSECURE_TRANSPORT` | in `prod`/`stage`, CSV of `database,cache,messaging,otlp,bucket,tls,http` or `all`: refusal becomes a warning |
| `DATABASE_SSL_MODE` | empty means `verify-full` (`disable` for a local host) |
| `DATABASE_SSL_ROOT_CERT` / `DATABASE_SSL_CERT` / `DATABASE_SSL_KEY` | server CA and client certificate (mTLS), PEM |
| `DATABASE_STATEMENT_TIMEOUT` / `DATABASE_QUERY_TIMEOUT` | PostgreSQL `statement_timeout`; query timeout when the `ctx` has no deadline (default 30s, negative disables) |
| `MESSAGING_TLS_CA_FILE` / `_CERT_FILE` / `_KEY_FILE` / `_SERVER_NAME` | any of them enables TLS to the broker (CA, mTLS, SNI) |
| `MESSAGING_TLS_INSECURE_SKIP_VERIFY` / `MESSAGING_ALLOW_PLAINTEXT_SASL` | development only; refused in `prod` |
| `MESSAGING_MAX_DELIVERIES` / `MESSAGING_HANDLER_TIMEOUT` | delivery limit before dead-lettering; handler timeout (default 5 min) |
| `HTTP_TLS_CERT_FILE` / `HTTP_TLS_KEY_FILE` / `HTTP_TLS_CLIENT_CA_FILE` / `HTTP_TLS_CLIENT_AUTH` | server TLS and mTLS |
| `HTTP_REQUIRE_TLS` | in `prod`/`stage`, an HTTP server without TLS fails `Build` |
| `HTTP_TRUSTED_PROXIES` | CSV of CIDRs (`private` = private networks); no proxy is trusted by default |
| `HTTP_ALLOWED_ORIGINS` | CORS origins, also trusted by the CSRF check (falls back to `ALLOWED_ORIGINS`) |
| `HTTP_MAX_CONCURRENT` / `HTTP_RATE_LIMIT_FAIL_CLOSED` | concurrency limiter slots; answer 503 when the rate-limit backend fails |
| `JWT_AUDIENCE` | `aud` issued and required |
| `IAM_LOGIN_MAX_ATTEMPTS` / `IAM_LOGIN_LOCKOUT` | failed logins per e-mail before lockout (default 5, negative disables) and its duration (default 15m) |
| `OTEL_EXPORTER_OTLP_INSECURE` | TLS is the default; `true` (or an `http://` endpoint) sends plaintext |
| `OTEL_EXPORTER_OTLP_CERTIFICATE` / `_CLIENT_CERTIFICATE` / `_CLIENT_KEY` | OTLP collector CA and mTLS |
| `BUCKET_PRESIGN_MAX_TTL` | lowers the presigned URL lifetime cap (7 days at most) |

---

## Development

### Prerequisites

* Go 1.26.6+
* Docker (for integration tests)

### Local workspace (go.work)

The repository uses `go work` to resolve dependencies between modules locally without needing to publish intermediate versions. Each `go.mod` also carries `replace` directives to its sibling modules.

```bash
# at the repository root
go work sync
```

The `go.work` file at the root lists all 22 modules in the workspace. Editors that support LSP (`gopls`) automatically detect the workspace.

### Tests

```bash
# every module in the workspace
go test $(go list -f '{{.Dir}}/...' -m)

# specific module
go test github.com/gofi-labs/gofi-sdk-go/netx/...

# with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

`make size-check` builds some examples (HTTP + database, database job, Kafka producer), prints their stripped size and fails when one links a package it must not (gRPC in a service without observability, pgx without the database component, ...).

Provider modules run shared contracts: `msq/msqtest` for brokers and `base/bucket/buckettest` for object stores, so behavior stays identical across backends.

### Minimum required coverage

Each module must maintain test coverage ≥ **90%**.

---

## Repository structure

```
gofi-sdk-go/
├── go.work              ← workspace (resolves modules locally); the root has no go.mod
├── Makefile             ← release helpers, size-check
│
├── gofi/                ← .../gofi (orchestrator)
│   ├── gofi.go          ← Builder, Service, Component and Runner contracts
│   ├── runtime.go       ← Runtime handed to Component.Start (closers, health, shared)
│   ├── builder.go       ← Builder implementation
│   ├── service.go       ← Service implementation (lifecycle, shutdown)
│   ├── component/       ← database, cache, session, messaging, observability, httpserver
│   └── config/          ← env → typed Config adapters
│       └── core/        ← timezone, logging, TLS applied by Build
│
├── base/                ← .../base
│   ├── environment/
│   ├── errs/
│   ├── secrets/
│   │   ├── awssm/       ← module
│   │   └── ocivault/    ← module
│   ├── bucket/
│   │   ├── mem/  file/  buckettest/
│   │   ├── s3/          ← module
│   │   └── oci/         ← module
│   ├── cloud/
│   │   ├── aws/         ← module
│   │   └── oci/         ← module
│   ├── mail/
│   ├── session/
│   ├── cronjob/
│   ├── timezone/
│   ├── observer/
│   ├── validator/
│   ├── common/
│   └── debug/
│
├── obs/                 ← .../obs
│   ├── logging/         ← slog console logger, Attach (no exporter)
│   ├── metrics/         ← instrument helpers, DB pool stats (no exporter)
│   ├── otel.go          ← Init: traces, metrics, logs over OTLP/gRPC
│   └── metric.go        ← deprecated wrappers of obs/metrics
│
├── sqln/                ← .../sqln
│   ├── connection/
│   ├── driver/          ← postgres/ mysql/ sqlserver/ oracle/
│   ├── query/
│   ├── statement/
│   ├── mapping/
│   ├── cache/
│   ├── pagination/
│   ├── criteria/
│   ├── filter/
│   ├── transaction/
│   ├── migrate/
│   └── rdsauth/         ← module
│
├── msq/                 ← .../msq
│   ├── types/
│   ├── port/
│   ├── core/
│   ├── worker/
│   ├── msqtest/
│   └── provider/        ← one module per provider
│       ├── kafka/
│       ├── rabbitmq/
│       ├── sqs/
│       ├── oci/
│       ├── redis/
│       └── nats/
│
├── netx/                ← .../netx
│   ├── http_server*.go
│   ├── http_client*.go  ← client, retries, Signature interface
│   └── awssign/         ← module (AWS SigV4 signer)
│
├── iam/                 ← .../iam
│   ├── types/
│   ├── core/
│   ├── port/
│   ├── config/
│   ├── middleware/
│   └── provider/        ← jwt/ redis/ memory/ rbac/ password/ bcrypt/ google/ microsoft/ oidc/
│
└── examples/            ← runnable samples, one module per project (netx/, sqln/, msq/, obs/, iam/)
```

---

## Conventions

* **SQL struct tags:** use `` `db:"column_name"` `` for automatic mapping via `sqln/mapping`.
* **Configuration:** libraries take explicit `Config` structs and never import `base/environment`; env mapping belongs in the root `config` package.
* **New cloud integrations:** a new provider or signer is its own module, depends on `base/cloud/{aws,oci}` for identity, and registers itself on import when it plugs into a registry (msq, bucket, secrets).
* **Logging:** use `obs/logging` for structured logging. Avoid `fmt.Println` in production code.
* **Tests:** minimum 90% coverage. Integration tests should be marked with `t.Skip(...)` and run only when infrastructure is available.
* **Injection vs convenience:** prefer `With*()` methods from the Builder when the application needs control over connection lifecycle.

---

## Author

<table>
  <tr>
    <td>
      <b>João Carvalho</b><br>
      Creator of gofi<br><br>
      GitHub — <a href="https://github.com/joaoprofile">github.com/joaoprofile</a><br>
      LinkedIn — <a href="https://www.linkedin.com/in/joaoprofile">linkedin.com/in/joaoprofile</a>
    </td>
  </tr>
</table>

Gofi was created by **João Carvalho** as a study of how to engineer the harness
around AI coding agents — and to apply it in professional projects: specialist
agents, a deterministic map of code and documents, and a front door that plans every
request before a model reads it. You are welcome to study it, use it and adapt it in
your own work; a mention of the author is appreciated.

Licensed under the [MIT License](LICENSE).

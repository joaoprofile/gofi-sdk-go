# gofi

Modular SDK for Go that provides the fundamental building blocks for building microservices: database, cache, messaging, HTTP, observability, authentication, and core utilities.

The project is organized as a **multi-module monorepo**: each subfolder is an independent Go module with its own `go.mod`. Consumers import only what they need, without pulling in dependencies from other domains.

---

## Architecture

### Overview

```
┌─────────────────────────────────────────────────────────┐
│               github.com/joaoprofile/gofi               │
│                   (main orchestrator)                   │
│                                                         │
│   gofi.New("service").                                  │
│       AddDatabase().AddCache().AddMessaging().Build()   │
└──────────────────────────┬──────────────────────────────┘
                           │ imports
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   ┌─────────┐       ┌──────────┐       ┌──────────┐
   │  base   │       │   obs    │       │   sqln   │
   └─────────┘       └──────────┘       └──────────┘
        ▲                  ▲                  ▲
        │ imports          │                  │
   ┌─────────┐       ┌──────────┐       ┌──────────┐
   │   msq   │       │   netx   │       │   iam    │
   └─────────┘       └──────────┘       └──────────┘
```

### Dependency hierarchy between modules

```
gofi   → base, obs, sqln, msq, netx, iam
obs    → base
sqln   → base, obs
msq    → base, obs
netx   → base, obs
iam    → base, obs
base   → (no internal dependencies)
```

`base` and `obs` are the foundation of the stack. All other modules import them, but there is never a circular dependency.

---

## Modules

### `gofi` — Main orchestrator

**Path:** `github.com/joaoprofile/gofi`

Entry point of the SDK. Exposes the `Builder` and `Service` interfaces and the public function `New(serviceName)`. The `Builder` offers two usage patterns:

* **Convenience (env-driven):** `Add*()` methods read configuration from the environment and create connections automatically.
* **Explicit injection:** `With*()` methods accept already built instances, allowing the consumer to fully control dependencies.

```go
// Convenience pattern — reads everything from environment
svc := gofi.New("my-service").
    AddDatabase().
    AddCache().
    AddMessaging().
    NewHttpServer(":8080").
    Build()

// Injection pattern — consumer provides instances
db  := sqln.MustConnect(sqln.ConfigFromEnv())
rdb := sqln.InstanceRedis()

svc := gofi.New("my-service").
    WithDatabase(db).
    WithCache(rdb).
    NewHttpServer(":8080").
    Build()
```

---

### `base` — Foundation

**Path:** `github.com/joaoprofile/gofi/base`

Infrastructure utilities and services used by all other modules. Has no internal dependencies within gofi.

| Sub-package   | Responsibility                                                     |
| ------------- | ------------------------------------------------------------------ |
| `environment` | Configuration singleton via environment variables (`.env`, OS env) |
| `session`     | Session management with Redis support                              |
| `cloud`       | Adapters for cloud providers (AWS, GCP, OCI)                       |
| `observer`    | Lifecycle hooks registry and global WaitGroup                      |
| `validator`   | Struct validation with custom tag support                          |
| `common`      | Utilities: timezone, strings, reflect, converters                  |
| `cronjob`     | Worker pool and periodic task scheduler                            |
| `debug`       | Diagnostic HTTP server (non-prod environments only)                |

```go
import "github.com/joaoprofile/gofi/base/environment"

env := environment.Instance()
fmt.Println(env.AppName, env.AppEnvironment)
```

---

### `obs` — Observability

**Path:** `github.com/joaoprofile/gofi/obs`

Integration with OpenTelemetry: traces, metrics, and structured logs via `slog`. Provides helpers to create metric instruments (histograms, counters, gauges) and a global logger.

| Export                                    | Description                               |
| ----------------------------------------- | ----------------------------------------- |
| `obs.Init(ctx, TeleConfig)`               | Initializes the OTel provider (OTLP/gRPC) |
| `obs.Meter()`                             | Returns the global `metric.Meter`         |
| `obs.NewFloat64Histogram(...)`            | Creates a float64 histogram               |
| `obs/logging.Info/Error/Warn/Debug/Fatal` | Global structured logging                 |

```go
import (
    "github.com/joaoprofile/gofi/obs"
    "github.com/joaoprofile/gofi/obs/logging"
)

tele, err := obs.Init(ctx, obs.TeleConfig{
    ServiceName:   "my-service",
    CollectorAddr: "otel-collector:4317",
})

logging.Info("service started", slog.String("port", ":8080"))
```

---

### `sqln` — Database and cache

**Path:** `github.com/joaoprofile/gofi/sqln`

Data access layer for SQL with support for PostgreSQL, MySQL, SQL Server, and Oracle. Includes pagination, dynamic filters, query caching (Redis or in-memory), and migrations.

| Sub-package   | Responsibility                                     |
| ------------- | -------------------------------------------------- |
| `connection`  | SQL connection pool with configuration and drivers |
| `migrate`     | Running migrations via filesystem                  |
| `mapping`     | Struct ↔ row mapping using `` `db:"col"` `` tag    |
| `pagination`  | `PageRequest`, `Sort`, `Page[T]`                   |
| `cache`       | Query result caching (Redis or in-memory)          |
| `criteria`    | Chainable query builder by predicates              |
| `filter`      | Dynamic filters from structs                       |
| `transaction` | Transaction management with context propagation    |

```go
import "github.com/joaoprofile/gofi/sqln"

type User struct {
    ID   int64  `db:"id"`
    Name string `db:"name"`
}

user, err := sqln.Find[User](ctx, "SELECT id, name FROM users WHERE id = $1", id).
    ExecuteUniqueQuery(db)
```

---

### `msq` — Messaging

**Path:** `github.com/joaoprofile/gofi/msq`

Message broker abstraction with support for multiple providers. The `Broker` interface is uniform — switching from Kafka to RabbitMQ only requires changing the provider.

| Provider           | Import                                              |
| ------------------ | --------------------------------------------------- |
| Apache Kafka       | `github.com/joaoprofile/gofi/msq/provider/kafka`    |
| RabbitMQ           | `github.com/joaoprofile/gofi/msq/provider/rabbitmq` |
| AWS SQS            | `github.com/joaoprofile/gofi/msq/provider/sqs`      |
| Oracle Cloud Queue | `github.com/joaoprofile/gofi/msq/provider/oci`      |
| Redis Pub/Sub      | `github.com/joaoprofile/gofi/msq/provider/redis`    |

```go
import (
    "github.com/joaoprofile/gofi/msq"
    "github.com/joaoprofile/gofi/msq/provider/kafka"
)

broker, _ := kafka.New(kafka.ConfigFromEnv())
svc, _    := msq.New(msq.Config{Broker: broker})

producer := svc.NewProducer()
producer.Publish(ctx, msq.NewMessageWithTopic("orders", order))
```

---

### `netx` — HTTP

**Path:** `github.com/joaoprofile/gofi/netx`

HTTP server and client based on `go-chi`. Includes ready-to-use middlewares.

| Export                          | Description                                   |
| ------------------------------- | --------------------------------------------- |
| `netx.NewServer(cfg)`           | Creates HTTP server with chi                  |
| `netx.NewClient(cfg)`           | Creates HTTP client with retry and rate limit |
| `netx.CORSMiddleware(cfg)`      | Configurable CORS middleware                  |
| `netx.NewRedisRateLimiter(...)` | Rate limiter via Redis                        |
| `netx.LoggingMiddleware`        | Structured request logging                    |
| `netx.SecurityHeaders`          | Security headers (CSP, HSTS, etc.)            |

```go
import "github.com/joaoprofile/gofi/netx"

server := netx.NewServer(&netx.WSConfig{ServerPort: ":8080"})
server.Use(netx.LoggingMiddleware, netx.SecurityHeaders)
server.AddHandlers(myRouter)
server.ListenAndServe()
```

---

### `iam` — Identity and authentication

**Path:** `github.com/joaoprofile/gofi/iam`

Identity, authentication, and authorization service. Supports JWT, session, RBAC, and multiple Identity Providers.

| Sub-package          | Responsibility                                                    |
| -------------------- | ----------------------------------------------------------------- |
| `core`               | `IAMService`: authentication, session, and RBAC                   |
| `port`               | Interfaces: `AuthPort`, `IDPAuthPort`, `SessionPort`, `TokenPort` |
| `provider/jwt`       | JWT token generation and validation                               |
| `provider/redis`     | Session persistence in Redis                                      |
| `provider/bcrypt`    | Password hashing                                                  |
| `provider/google`    | Google OAuth                                                      |
| `provider/microsoft` | Microsoft OAuth                                                   |
| `provider/oidc`      | Generic OIDC provider                                             |
| `middleware`         | Authentication middleware for HTTP and gRPC                       |

```go
import "github.com/joaoprofile/gofi/iam"

svc := iam.New(iam.Config{
    Security: iam.SecurityConfig{JWTSecret: "..."},
})

token, err := svc.Login(ctx, credentials)
```

---

## Installation

### Full usage via orchestrator

```bash
go get github.com/joaoprofile/gofi
```

### Per-module usage (only what you need)

```bash
go get github.com/joaoprofile/gofi/netx    # HTTP only
go get github.com/joaoprofile/gofi/sqln    # database only
go get github.com/joaoprofile/gofi/msq     # messaging only
go get github.com/joaoprofile/gofi/obs     # observability only
go get github.com/joaoprofile/gofi/iam     # authentication only
go get github.com/joaoprofile/gofi/base    # base utilities only
```

---

## Configuration via environment variables

The `base/environment` module is the central configuration point. All modules read configuration from environment variables at startup.

| Prefix        | Module    | Examples                                   |
| ------------- | --------- | ------------------------------------------ |
| `APP_*`       | gofi      | `APP_NAME`, `APP_ENV`                      |
| `DATABASE_*`  | sqln      | `DATABASE_URI`, `DATABASE_DRIVER`          |
| `CACHE_*`     | sqln/base | `CACHE_URI`, `CACHE_TYPE`                  |
| `MESSAGING_*` | msq       | `MESSAGING_PROVIDER`, `MESSAGING_BROKER_*` |
| `OTEL_*`      | obs       | `OTEL_EXPORTER_OTLP_ENDPOINT`              |
| `IAM_*`       | iam       | `IAM_JWT_SECRET`, `IAM_SESSION_TTL`        |

---

## Development

### Prerequisites

* Go 1.25+
* Docker (for integration tests)

### Local workspace (go.work)

The repository uses `go work` to resolve dependencies between modules locally without needing to publish intermediate versions:

```bash
# at the repository root
go work sync
```

The `go.work` file at the root lists all modules in the workspace. Editors that support LSP (`gopls`) automatically detect the workspace.

### Tests

```bash
# all modules
go test ./...

# specific module
go test github.com/joaoprofile/gofi/netx/...

# with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Minimum required coverage

Each module must maintain test coverage ≥ **90%**.

---

## Repository structure

```
gofi/
├── go.work              ← workspace (resolves modules locally)
├── go.mod               ← main module (github.com/joaoprofile/gofi)
├── gofi.go              ← Builder and Service interfaces
├── builder.go           ← Builder implementation (gofiInstance)
├── service.go           ← Service implementation (gofiInstance)
│
├── base/                ← github.com/joaoprofile/gofi/base
│   ├── go.mod
│   ├── environment/
│   ├── session/
│   ├── cloud/
│   ├── observer/
│   ├── validator/
│   ├── common/
│   ├── cronjob/
│   └── debug/
│
├── obs/                 ← github.com/joaoprofile/gofi/obs
│   ├── go.mod
│   ├── logging/
│   ├── otel.go
│   └── metric.go
│
├── sqln/                ← github.com/joaoprofile/gofi/sqln
│   ├── go.mod
│   ├── connection/
│   ├── driver/
│   ├── mapping/
│   ├── cache/
│   ├── pagination/
│   ├── criteria/
│   ├── filter/
│   ├── transaction/
│   └── migrate/
│
├── msq/                 ← github.com/joaoprofile/gofi/msq
│   ├── go.mod
│   ├── types/
│   ├── core/
│   ├── port/
│   ├── worker/
│   └── provider/
│       ├── kafka/
│       ├── rabbitmq/
│       ├── sqs/
│       ├── oci/
│       └── redis/
│
├── netx/                ← github.com/joaoprofile/gofi/netx
│   └── go.mod
│
└── iam/                 ← github.com/joaoprofile/gofi/iam
    ├── go.mod
    ├── types/
    ├── core/
    ├── port/
    ├── config/
    ├── middleware/
    └── provider/
```

---

## Conventions

* **SQL struct tags:** use `` `db:"column_name"` `` for automatic mapping via `sqln/mapping`.
* **Configuration:** prefer reading via `environment.Instance()` inside each module’s `ConfigFromEnv()` methods.
* **Logging:** use `obs/logging` for structured logging. Avoid `fmt.Println` in production code.
* **Tests:** minimum 90% coverage. Integration tests should be marked with `t.Skip(...)` and run only when infrastructure is available.
* **Injection vs convenience:** prefer `With*()` methods from the Builder when the application needs control over connection lifecycle.

# gofi

SDK modular para Go que fornece os blocos fundamentais para construção de microsserviços: banco de dados, cache, mensageria, HTTP, observabilidade, autenticação e utilitários de base.

O projeto é organizado como um **monorepo multi-módulo**: cada sub-pasta é um módulo Go independente com seu próprio `go.mod`. O consumidor importa apenas o que precisa, sem arrastar dependências de outros domínios.

---

## Arquitetura

### Visão geral

```
┌─────────────────────────────────────────────────────────┐
│               github.com/joaoprofile/gofi               │
│                   (orquestrador principal)               │
│                                                         │
│   gofi.New("service").                                  │
│       AddDatabase().AddCache().AddMessaging().Build()   │
└──────────────────────────┬──────────────────────────────┘
                           │ importa
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   ┌─────────┐       ┌──────────┐       ┌──────────┐
   │  base   │       │   obs    │       │   sqln   │
   └─────────┘       └──────────┘       └──────────┘
        ▲                  ▲                  ▲
        │ importa          │                  │
   ┌─────────┐       ┌──────────┐       ┌──────────┐
   │   msq   │       │   netx   │       │   iam    │
   └─────────┘       └──────────┘       └──────────┘
```

### Hierarquia de dependências entre módulos

```
gofi   → base, obs, sqln, msq, netx, iam
obs    → base
sqln   → base, obs
msq    → base, obs
netx   → base, obs
iam    → base, obs
base   → (sem dependências internas)
```

`base` e `obs` são a fundação da pilha. Todos os outros módulos os importam, mas nunca há dependência circular.

---

## Módulos

### `gofi` — Orquestrador principal
**Path:** `github.com/joaoprofile/gofi`

Ponto de entrada do SDK. Expõe as interfaces `Builder` e `Service` e a função pública `New(serviceName)`. O `Builder` oferece dois padrões de uso:

- **Convenience (env-driven):** métodos `Add*()` leem a configuração do ambiente e criam as conexões automaticamente.
- **Injeção explícita:** métodos `With*()` aceitam instâncias já construídas, permitindo ao consumidor controlar totalmente as dependências.

```go
// Padrão convenience — lê tudo do ambiente
svc := gofi.New("my-service").
    AddDatabase().
    AddCache().
    AddMessaging().
    NewHttpServer(":8080").
    Build()

// Padrão injeção — consumidor traz as instâncias
db  := sqln.MustConnect(sqln.ConfigFromEnv())
rdb := sqln.InstanceRedis()

svc := gofi.New("my-service").
    WithDatabase(db).
    WithCache(rdb).
    NewHttpServer(":8080").
    Build()
```

---

### `base` — Fundação
**Path:** `github.com/joaoprofile/gofi/base`

Utilitários e serviços de infraestrutura usados por todos os outros módulos. Não possui dependências internas ao gofi.

| Sub-pacote | Responsabilidade |
|---|---|
| `environment` | Singleton de configuração via variáveis de ambiente (`.env`, OS env) |
| `session` | Gerenciamento de sessão com suporte a Redis |
| `cloud` | Adaptadores para provedores cloud (AWS, GCP, OCI) |
| `observer` | Registry de lifecycle hooks e WaitGroup global |
| `validator` | Validação de structs com suporte a tags customizadas |
| `common` | Utilitários: timezone, strings, reflect, conversores |
| `cronjob` | Worker pool e scheduler de tarefas periódicas |
| `debug` | Servidor HTTP de diagnóstico (apenas ambientes não-prod) |

```go
import "github.com/joaoprofile/gofi/base/environment"

env := environment.Instance()
fmt.Println(env.AppName, env.AppEnvironment)
```

---

### `obs` — Observabilidade
**Path:** `github.com/joaoprofile/gofi/obs`

Integração com OpenTelemetry: traces, métricas e logs estruturados via `slog`. Expõe helpers para criar instrumentos de métricas (histogramas, contadores, gauges) e o logger global.

| Exportação | Descrição |
|---|---|
| `obs.Init(ctx, TeleConfig)` | Inicializa o provider OTel (OTLP/gRPC) |
| `obs.Meter()` | Retorna o `metric.Meter` global |
| `obs.NewFloat64Histogram(...)` | Cria histograma de float64 |
| `obs/logging.Info/Error/Warn/Debug/Fatal` | Logging estruturado global |

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

### `sqln` — Banco de dados e cache
**Path:** `github.com/joaoprofile/gofi/sqln`

Camada de acesso a dados SQL com suporte a PostgreSQL, MySQL, SQL Server e Oracle. Inclui paginação, filtros dinâmicos, cache de queries (Redis ou in-memory) e migrations.

| Sub-pacote | Responsabilidade |
|---|---|
| `connection` | Pool de conexões SQL com configuração e drivers |
| `migrate` | Execução de migrations via sistema de arquivos |
| `mapping` | Mapeamento struct ↔ row usando a tag `` `db:"col"` `` |
| `pagination` | `PageRequest`, `Sort`, `Page[T]` |
| `cache` | Cache de resultado de query (Redis ou in-memory) |
| `criteria` | Query builder encadeável por predicados |
| `filter` | Filtros dinâmicos a partir de structs |
| `transaction` | Gerenciamento de transação com context propagation |

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

### `msq` — Mensageria
**Path:** `github.com/joaoprofile/gofi/msq`

Abstração de broker de mensagens com suporte a múltiplos provedores. A interface `Broker` é uniforme — trocar de Kafka para RabbitMQ exige apenas mudar o provider.

| Provider | Import |
|---|---|
| Apache Kafka | `github.com/joaoprofile/gofi/msq/provider/kafka` |
| RabbitMQ | `github.com/joaoprofile/gofi/msq/provider/rabbitmq` |
| AWS SQS | `github.com/joaoprofile/gofi/msq/provider/sqs` |
| Oracle Cloud Queue | `github.com/joaoprofile/gofi/msq/provider/oci` |
| Redis Pub/Sub | `github.com/joaoprofile/gofi/msq/provider/redis` |

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

Servidor e cliente HTTP baseados em `go-chi`. Inclui middlewares prontos para uso.

| Exportação | Descrição |
|---|---|
| `netx.NewServer(cfg)` | Cria servidor HTTP com chi |
| `netx.NewClient(cfg)` | Cria cliente HTTP com retry e rate limit |
| `netx.CORSMiddleware(cfg)` | Middleware CORS configurável |
| `netx.NewRedisRateLimiter(...)` | Rate limiter via Redis |
| `netx.LoggingMiddleware` | Log estruturado de requests |
| `netx.SecurityHeaders` | Headers de segurança (CSP, HSTS, etc.) |

```go
import "github.com/joaoprofile/gofi/netx"

server := netx.NewServer(&netx.WSConfig{ServerPort: ":8080"})
server.Use(netx.LoggingMiddleware, netx.SecurityHeaders)
server.AddHandlers(myRouter)
server.ListenAndServe()
```

---

### `iam` — Identidade e autenticação
**Path:** `github.com/joaoprofile/gofi/iam`

Serviço de identidade, autenticação e autorização. Suporta JWT, sessão, RBAC e múltiplos Identity Providers.

| Sub-pacote | Responsabilidade |
|---|---|
| `core` | `IAMService`: autenticação, sessão e RBAC |
| `port` | Interfaces: `AuthPort`, `IDPAuthPort`, `SessionPort`, `TokenPort` |
| `provider/jwt` | Geração e validação de tokens JWT |
| `provider/redis` | Sessão persistida em Redis |
| `provider/bcrypt` | Hash de senhas |
| `provider/google` | OAuth com Google |
| `provider/microsoft` | OAuth com Microsoft |
| `provider/oidc` | Provider OIDC genérico |
| `middleware` | Middleware de autenticação para HTTP e gRPC |

```go
import "github.com/joaoprofile/gofi/iam"

svc := iam.New(iam.Config{
    Security: iam.SecurityConfig{JWTSecret: "..."},
})

token, err := svc.Login(ctx, credentials)
```

---

## Instalação

### Uso completo via orquestrador

```bash
go get github.com/joaoprofile/gofi
```

### Uso por módulo (apenas o que precisar)

```bash
go get github.com/joaoprofile/gofi/netx    # só HTTP
go get github.com/joaoprofile/gofi/sqln    # só banco de dados
go get github.com/joaoprofile/gofi/msq     # só mensageria
go get github.com/joaoprofile/gofi/obs     # só observabilidade
go get github.com/joaoprofile/gofi/iam     # só autenticação
go get github.com/joaoprofile/gofi/base    # só utilitários base
```

---

## Configuração via variáveis de ambiente

O módulo `base/environment` é o ponto central de configuração. Todos os módulos leem configuração de variáveis de ambiente no startup.

| Prefixo | Módulo | Exemplos |
|---|---|---|
| `APP_*` | gofi | `APP_NAME`, `APP_ENV` |
| `DATABASE_*` | sqln | `DATABASE_URI`, `DATABASE_DRIVER` |
| `CACHE_*` | sqln/base | `CACHE_URI`, `CACHE_TYPE` |
| `MESSAGING_*` | msq | `MESSAGING_PROVIDER`, `MESSAGING_BROKER_*` |
| `OTEL_*` | obs | `OTEL_EXPORTER_OTLP_ENDPOINT` |
| `IAM_*` | iam | `IAM_JWT_SECRET`, `IAM_SESSION_TTL` |

---

## Desenvolvimento

### Pré-requisitos

- Go 1.25+
- Docker (para testes de integração)

### Workspace local (go.work)

O repositório usa `go work` para resolver dependências entre módulos localmente sem precisar publicar versões intermediárias:

```bash
# na raiz do repositório
go work sync
```

O arquivo `go.work` na raiz lista todos os módulos do workspace. Editores que suportam LSP (`gopls`) detectam automaticamente o workspace.

### Testes

```bash
# todos os módulos
go test ./...

# módulo específico
go test github.com/joaoprofile/gofi/netx/...

# com cobertura
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Cobertura mínima exigida

Cada módulo deve manter cobertura de testes ≥ **90%**.

---

## Estrutura do repositório

```
gofi/
├── go.work              ← workspace (resolve módulos localmente)
├── go.mod               ← módulo principal (github.com/joaoprofile/gofi)
├── gofi.go              ← interfaces Builder e Service
├── builder.go           ← implementação do Builder (gofiInstance)
├── service.go           ← implementação do Service (gofiInstance)
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

## Convenções

- **Struct tags SQL:** use `` `db:"nome_da_coluna"` `` para mapeamento automático via `sqln/mapping`.
- **Configuração:** prefira leitura via `environment.Instance()` nos métodos `ConfigFromEnv()` de cada módulo.
- **Logging:** use `obs/logging` para logging estruturado. Evite `fmt.Println` em código de produção.
- **Testes:** cobertura mínima de 90%. Testes de integração devem ser marcados com `t.Skip(...)` e executados apenas com infraestrutura disponível.
- **Injeção vs convenience:** prefira os métodos `With*()` do Builder quando a aplicação precisa de controle sobre o ciclo de vida das conexões.

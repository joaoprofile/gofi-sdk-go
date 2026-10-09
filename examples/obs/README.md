# obs — observability example

An instrumented service that exports **traces, metrics and logs** over OTLP
to a local Grafana stack. It covers what a real service has to instrument:

| What | Where | Shows |
|------|-------|-------|
| HTTP handlers | [handler/order.go](handler/order.go) | server span from httpx, business attributes, a flow split into steps, counters and histograms, trace-aware logs |
| Outgoing HTTP call | `OrderHandler.charge` | `httpx.HttpClient` creates the client span and propagates `traceparent` |
| Called service | [handler/payment.go](handler/payment.go) | continues the caller's trace, span events |
| Async flow (queue) | [worker/](worker) | trace context injected into message headers and extracted by the consumer; observable gauge |
| Scheduled job | [job/archive.go](job/archive.go) | one root trace per run, a child span per batch, duration/outcome metrics |
| Shared helpers | [telemetry/](telemetry) | tracer, instruments created once, `Step`, `Fail` |
| Wiring | [main.go](main.go), [runner.go](runner.go) | `gofi.New` with the `observability` and `httpserver` components; background loops as `gofi.Runner` |

```
app (go run .) ──OTLP gRPC :4317──> otel-collector ──> Tempo       traces
                                                   ──> Prometheus  metrics (scrapes :8889)
                                                   ──> Loki        logs
                                    Grafana :3000 reads all three, linked by trace_id
```

## Run

```sh
cd examples/obs
docker compose up -d
go run .
```

Configuration comes from [.env](.env), read automatically outside
`prod`/`stage`; variables set in the shell win. A built-in load generator
sends orders every 250 ms, so the dashboards fill right away
(`LOADGEN=false go run .` turns it off). Point the app at another collector
with `OTEL_EXPORTER_OTLP_ENDPOINT`.

| URL | What |
|-----|------|
| http://localhost:3000 | Grafana (no login): **Dashboards → gofi → gofi / obs-demo** |
| http://localhost:9090 | Prometheus |
| http://localhost:8889/metrics | raw metrics as the collector exposes them |

Try it by hand:

```sh
curl -X POST localhost:8080/orders -H 'Content-Type: application/json' -d '{"amount":120,"payment_method":"pix"}'
curl -X POST localhost:8080/orders -H 'Content-Type: application/json' -d '{"amount":-1,"payment_method":"pix"}'   # 400
curl localhost:8080/orders/ord-1
```

`docker compose down` stops the stack (data is not persisted).

## What you see in Grafana

**Trace of `POST /orders`** (Explore → Tempo, or click a trace in the dashboard):

```
POST /orders                 server    (httpx)
└─ order.place               internal  (the flow)
   ├─ order.validate
   ├─ stock.reserve
   ├─ HTTP POST              client    (httpx.HttpClient)
   │  └─ POST /payments      server    (same trace across the HTTP call)
   └─ publish orders         producer
      └─ process orders      consumer  (other goroutine, same trace)
         └─ notification.send
```

Failed steps are red and carry the error as an event. Useful TraceQL:

```
{resource.service.name="obs-demo" && status=error}
{span.order.payment_method="pix" && duration > 300ms}
{name="job archive-orders"}
```

**Logs** (Explore → Loki): `{service_name="obs-demo"} | severity_text="ERROR"`.
Only records at or above the logger level (Info by default) are exported,
the same as the console.
Each line carries `trace_id`; the **View trace** link opens it in Tempo, and
from a span, **Logs for this span** goes back.

**Metrics**: the dashboard has HTTP rate/latency/errors by route, business
counters, queue depth, job runs and Go runtime. Exemplar dots on the latency
panel open the trace of that exact request.

## How to instrument

### 1. Setup: the observability component

```go
svc, err := gofi.New("obs-demo").
    With(
        observability.New(),                 // OTEL_EXPORTER_OTLP_ENDPOINT
        httpserver.New(":8080").Handlers(h), // spans and metrics per route
        newRunner("queue consumer", consumer.Run),
    ).
    Build()
svc.ListenAndServe()
```

`Build` sets up the logger from the environment (`APP_ENVIRONMENT`,
`LOG_LEVEL`), then `observability` starts OpenTelemetry: traces, metrics and
logs over one gRPC connection, the W3C propagator and the Go runtime metrics
(`go_goroutine_count`, `go_memory_used_bytes`...). The service name comes
from `gofi.New`, the version from `APP_VERSION` and the environment from
`APP_ENVIRONMENT`.
`OTEL_EXPORTER_OTLP_ENDPOINT` empty skips telemetry with a warning.

On SIGINT/SIGTERM, `ListenAndServe` stops the server and the runners first
and flushes telemetry last, so the shutdown is exported too.

Instruments and tracers can be created before `Build`: the global
OpenTelemetry providers start as no-ops and switch to the exporting ones when
the component starts.

### 2. HTTP handlers

httpx already opens a server span named after the route (`POST /orders`) and
records `http.server.request.duration` labeled with `http_route` for every
request. Add what is specific to your code:

```go
// Business attributes on the request span: searchable in Tempo.
trace.SpanFromContext(ctx).SetAttributes(attribute.String("order.payment_method", in.PaymentMethod))

// Logger with trace_id/span_id; use the *Context methods.
log := logging.FromContext(ctx)
log.InfoContext(ctx, "order created", "order_id", order.ID)
```

### 3. Flows: one span per step

```go
ctx, span := telemetry.Tracer().Start(ctx, "order.place")
defer span.End()

err := telemetry.Step(ctx, "stock.reserve", reserveStock) // child span, red on error
```

Always pass the returned `ctx` down: that is what makes the next span a
child. On errors, `span.RecordError` alone does not mark the span as failed;
`telemetry.Fail` also sets the status.

### 4. Calls to other services

Use `httpx.HttpClient` (or any `otelhttp` transport) and pass the request
`ctx`. The `traceparent` header goes along and the other service continues
the same trace.

### 5. Queues and async work

A goroutine or a broker breaks the `ctx` chain, so carry the context in the
message:

```go
// producer
otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(msg.Headers))

// consumer
ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(msg.Headers))
ctx, span := tracer.Start(ctx, "process orders", trace.WithSpanKind(trace.SpanKindConsumer))
```

The same applies to Kafka, RabbitMQ or SQS headers.

### 6. Background loops and jobs

A consumer or a scheduler is a loop that must start with the service and
stop before telemetry is flushed. [runner.go](runner.go) adapts a
`func(ctx)` loop to `gofi.Runner`; pass it to `With` like any component.

No request starts a job, so each run is its own trace
(`trace.WithNewRoot()`). Record at least duration and outcome per run: this
is enough to alert on "stopped running", "failing" and "getting slower".

### 7. Metrics

```go
// Create once at startup (telemetry.NewMetrics), reuse everywhere.
ordersCreated, _ := metrics.NewInt64Counter("orders.created", "Orders placed")
ordersCreated.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "created")))
```

| Instrument | Use for | Example |
|------------|---------|---------|
| Counter | things that happen | `orders_created_total` |
| UpDownCounter | things in progress | `orders_in_progress` |
| Histogram | durations and sizes (percentiles) | `jobs_run_duration_seconds` |
| Observable gauge | a value you can read when asked | `queue_depth` |

Rules that save you trouble:

- **Low cardinality.** Attributes take a few known values (`status`, `outcome`).
  Never IDs, emails or amounts: put those on spans and logs.
- **Histogram buckets.** The SDK default buckets suit milliseconds. For seconds,
  pass `metric.WithExplicitBucketBoundaries`, or every value lands in the first bucket.
- **No `job` or `instance` attributes.** Prometheus owns those labels; the
  collector drops the whole series. Use `job.name`.
- **Names.** Dots in code, underscores and suffixes in Prometheus:
  `jobs.run.duration` with unit `s` becomes `jobs_run_duration_seconds`.

## Stack files

| File | Role |
|------|------|
| [compose.yaml](compose.yaml) | collector, Prometheus, Tempo, Loki, Grafana |
| [deploy/otel-collector.yaml](deploy/otel-collector.yaml) | OTLP in; Tempo, Prometheus exporter and Loki out |
| [deploy/prometheus.yml](deploy/prometheus.yml) | scrapes the collector, keeping `job=<service.name>` |
| [deploy/tempo.yaml](deploy/tempo.yaml), [deploy/loki.yaml](deploy/loki.yaml) | single-node local storage |
| [deploy/grafana/](deploy/grafana) | datasources with trace ↔ log ↔ metric links, and the dashboard |

In production, the app only needs `OTEL_EXPORTER_OTLP_ENDPOINT`. The exporter
uses TLS by default; a plaintext collector (a local sidecar, for example) needs
an `http://` endpoint or `OTEL_EXPORTER_OTLP_INSECURE=true`. The backends can
be any OTLP-compatible vendor.

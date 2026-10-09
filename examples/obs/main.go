// Command obs is an instrumented service: HTTP handlers, an internal HTTP
// call, an async queue consumer and a scheduled job, all exporting traces,
// metrics and logs over OTLP to the stack in compose.yaml.
package main

import (
	"context"
	"log"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/examples/obs/handler"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/job"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/store"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/telemetry"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/worker"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/httpserver"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/observability"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

const baseURL = "http://localhost:8080"

func main() {
	// 1. Business instruments, created once and shared. They use the global
	// OpenTelemetry providers, which start as no-ops and switch to the
	// exporting ones when observability starts in Build.
	m, err := telemetry.NewMetrics()
	if err != nil {
		log.Fatalf("create metrics: %v", err)
	}

	orders := store.New()
	queue, err := worker.NewQueue(100)
	if err != nil {
		log.Fatalf("create queue: %v", err)
	}
	// httpx.HttpClient propagates the trace context and records client spans.
	payments, err := httpx.NewClient(&httpx.HttpClientConfig{
		Name:    "payments",
		BaseURL: baseURL,
		Timeout: 2 * time.Second,
		Retries: 1,
	})
	if err != nil {
		log.Fatalf("create payments client: %v", err)
	}

	components := []gofi.Component{
		// 2. Traces, metrics and logs to OTEL_EXPORTER_OTLP_ENDPOINT over one
		// gRPC connection, plus Go runtime metrics and the W3C propagator.
		// It starts first and is flushed last, after the shutdown logs.
		observability.New(),

		// 3. HTTP server: httpx names each request span after its route
		// ("POST /orders") and records http.server.* metrics with http_route.
		httpserver.New(":8080").Handlers(
			handler.NewOrderHandler(orders, queue, payments, m),
			handler.NewPaymentHandler(m),
		),

		// 4. Background work, started and stopped with the server.
		newRunner("queue consumer", worker.NewConsumer(queue, orders, m).Run),
		newRunner("archive job", job.NewArchive(orders, m, 15*time.Second).Run),
		newRunner("load generator", func(ctx context.Context) { generateLoad(ctx, baseURL) }),
	}

	// Build loads .env, sets up logging and starts the components.
	svc, err := gofi.New("obs-demo").With(components...).Build()
	if err != nil {
		log.Fatal(err)
	}

	// Blocks until SIGINT/SIGTERM, stops the runners, then flushes telemetry.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

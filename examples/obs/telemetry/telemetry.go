// Package telemetry holds what the service needs to instrument itself: the
// tracer, the business metrics and small helpers shared by handlers, jobs and
// workers. obs.Init installs the exporting providers; this package only uses
// the OpenTelemetry API, so it records nothing (no-op) until then.
package telemetry

import (
	"context"
	"errors"

	"github.com/gofi-labs/gofi-sdk-go/obs/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// scope identifies this code as the instrumentation source of its spans.
const scope = "github.com/gofi-labs/gofi-sdk-go/examples/obs"

// Tracer returns the service tracer from the global provider set by obs.Init.
func Tracer() trace.Tracer { return otel.Tracer(scope) }

// Metrics are the business instruments of the service. Create them once at
// startup and share them: an instrument is a long-lived object, not a value
// to build per request.
//
// Names use dots (OpenTelemetry style); Prometheus sees them with underscores
// plus unit and type suffixes: orders.created -> orders_created_total.
// Never name an attribute "job" or "instance": Prometheus owns those labels
// and the collector drops the whole series on the clash.
type Metrics struct {
	OrdersCreated     metric.Int64Counter       // orders_created_total{status,payment_method}
	OrdersInProgress  metric.Int64UpDownCounter // orders_in_progress
	OrderAmount       metric.Float64Histogram   // orders_amount_bucket{payment_method}
	PaymentsProcessed metric.Int64Counter       // payments_processed_total{outcome}
	MessagesProcessed metric.Int64Counter       // messages_processed_total{queue,outcome}
	JobDuration       metric.Float64Histogram   // jobs_run_duration_seconds_bucket{job_name,outcome}
	JobItems          metric.Int64Counter       // jobs_items_processed_total{job_name}
}

func NewMetrics() (*Metrics, error) {
	var m Metrics
	var errs [7]error
	m.OrdersCreated, errs[0] = metrics.NewInt64Counter("orders.created", "Orders placed, by final status")
	m.OrdersInProgress, errs[1] = metrics.NewInt64UpDownCounter("orders.in_progress", "Orders being placed right now")
	m.OrderAmount, errs[2] = metrics.Meter().Float64Histogram("orders.amount",
		metric.WithDescription("Amount of the orders created"),
		metric.WithExplicitBucketBoundaries(10, 25, 50, 100, 250, 500, 1000),
	)
	m.PaymentsProcessed, errs[3] = metrics.NewInt64Counter("payments.processed", "Payment attempts, by outcome")
	m.MessagesProcessed, errs[4] = metrics.NewInt64Counter("messages.processed", "Queue messages consumed, by outcome")
	// The SDK default buckets (0..10000) suit milliseconds; durations in
	// seconds need their own boundaries or every value lands in the first one.
	m.JobDuration, errs[5] = metrics.Meter().Float64Histogram("jobs.run.duration",
		metric.WithDescription("Duration of each job run"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	m.JobItems, errs[6] = metrics.NewInt64Counter("jobs.items.processed", "Items handled by jobs")
	return &m, errors.Join(errs[:]...)
}

// Step runs fn inside a child span named name. It is the building block of a
// flow: each step shows up as a bar in the trace, with its own duration and
// error, under the span found in ctx.
func Step(ctx context.Context, name string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, span := Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
	defer span.End()
	if err := fn(ctx); err != nil {
		Fail(span, err)
		return err
	}
	return nil
}

// Fail marks span as failed: the error becomes a span event and the span
// turns red in Tempo. Recording the error alone does not change the status.
func Fail(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

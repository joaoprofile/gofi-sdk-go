package core

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/joaoprofile/gofi-sdk-go/msq"

// telemetry resolves the global providers lazily so obs.Init may run after
// the service is created.
type telemetry struct {
	system string
}

func (t telemetry) tracer() trace.Tracer { return otel.Tracer(instrumentationName) }

func (t telemetry) attrs(op attribute.KeyValue, destination string) []attribute.KeyValue {
	kv := []attribute.KeyValue{op}
	if t.system != "" {
		kv = append(kv, attribute.String(string(semconv.MessagingSystemKey), t.system))
	}
	if destination != "" {
		kv = append(kv, semconv.MessagingDestinationName(destination))
	}
	return kv
}

// instruments follow the OpenTelemetry messaging semantic conventions.
type instruments struct {
	processDuration metric.Float64Histogram
	opDuration      metric.Float64Histogram
	consumed        metric.Int64Counter
	sent            metric.Int64Counter
}

func newInstruments() instruments {
	m := otel.Meter(instrumentationName)
	var in instruments
	// Instrument creation only fails on invalid names; the no-op fallbacks keep the pipeline running.
	in.processDuration, _ = m.Float64Histogram("messaging.process.duration",
		metric.WithUnit("s"), metric.WithDescription("Duration of processing operation."))
	in.opDuration, _ = m.Float64Histogram("messaging.client.operation.duration",
		metric.WithUnit("s"), metric.WithDescription("Duration of messaging operation initiated by a producer or consumer client."))
	in.consumed, _ = m.Int64Counter("messaging.client.consumed.messages",
		metric.WithUnit("{message}"), metric.WithDescription("Number of messages that were delivered to the application."))
	in.sent, _ = m.Int64Counter("messaging.client.sent.messages",
		metric.WithUnit("{message}"), metric.WithDescription("Number of messages producer attempted to send to the broker."))
	return in
}

// extract returns ctx carrying the producer's trace context from the headers.
func extract(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(headers))
}

// inject writes the current trace context into the headers.
func inject(ctx context.Context, headers map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
}

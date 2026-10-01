// Package worker shows asynchronous flows: a producer puts messages on a
// queue and a consumer handles them later, in another goroutine, and the
// trace still links both sides.
package worker

import (
	"context"
	"errors"

	"github.com/joaoprofile/gofi-sdk-go/examples/obs/telemetry"
	"github.com/joaoprofile/gofi-sdk-go/obs/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const queueName = "orders"

var errQueueFull = errors.New("queue full")

// Message stands in for a broker message. Headers carry the W3C trace
// context the same way Kafka, RabbitMQ or SQS message headers would.
type Message struct {
	Type    string
	OrderID string
	Headers map[string]string
}

// Queue is an in-memory queue; replace it with msq and the pattern is the same.
type Queue struct {
	ch chan Message
}

func NewQueue(size int) (*Queue, error) {
	q := &Queue{ch: make(chan Message, size)}
	// An observable gauge is read by the SDK on every export through the
	// callback: right for values you can look up (queue depth, pool size,
	// cache entries) instead of tracking every change.
	_, err := metrics.Meter().Int64ObservableGauge("queue.depth",
		metric.WithDescription("Messages waiting in the queue"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(len(q.ch)), metric.WithAttributes(attribute.String("queue", queueName)))
			return nil
		}),
	)
	return q, err
}

// Publish injects the current trace context into the message headers.
func (q *Queue) Publish(ctx context.Context, msg Message) error {
	ctx, span := telemetry.Tracer().Start(ctx, "publish "+queueName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("memory"),
			semconv.MessagingDestinationName(queueName),
			attribute.String("message.type", msg.Type),
		),
	)
	defer span.End()

	msg.Headers = map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(msg.Headers))

	select {
	case q.ch <- msg:
		return nil
	default:
		telemetry.Fail(span, errQueueFull)
		return errQueueFull
	}
}

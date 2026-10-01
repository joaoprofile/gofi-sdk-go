package worker

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/examples/obs/store"
	"github.com/joaoprofile/gofi-sdk-go/examples/obs/telemetry"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

var errNotifyFailed = errors.New("notification provider unavailable")

// Consumer sends a notification for each paid order.
type Consumer struct {
	queue   *Queue
	store   *store.Store
	metrics *telemetry.Metrics
}

func NewConsumer(q *Queue, s *store.Store, m *telemetry.Metrics) *Consumer {
	return &Consumer{queue: q, store: s, metrics: m}
}

// Run consumes until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.queue.ch:
			c.handle(ctx, msg)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, msg Message) {
	// Extract the producer's context from the headers: the consumer span joins
	// the same trace, so Tempo shows HTTP request -> publish -> process.
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(msg.Headers))
	ctx, span := telemetry.Tracer().Start(ctx, "process "+queueName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("memory"),
			semconv.MessagingDestinationName(queueName),
			attribute.String("order.id", msg.OrderID),
		),
	)
	defer span.End()

	err := telemetry.Step(ctx, "notification.send", func(context.Context) error {
		time.Sleep(time.Duration(10+rand.IntN(40)) * time.Millisecond) // #nosec G404 -- simulated load, not a secret
		if rand.IntN(100) < 5 {                                        // #nosec G404 -- simulated load, not a secret
			return errNotifyFailed
		}
		return nil
	})
	if err == nil {
		err = c.store.SetStatus(msg.OrderID, store.StatusNotified)
	}

	outcome := "success"
	log := logging.FromContext(ctx)
	if err != nil {
		outcome = "error"
		telemetry.Fail(span, err)
		log.ErrorContext(ctx, "message failed", "order_id", msg.OrderID, "error", err)
	} else {
		log.DebugContext(ctx, "customer notified", "order_id", msg.OrderID)
	}
	c.metrics.MessagesProcessed.Add(ctx, 1, metric.WithAttributes(
		attribute.String("queue", queueName),
		attribute.String("outcome", outcome),
	))
}

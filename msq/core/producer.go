package core

import (
	"context"
	"maps"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// producer decorates a provider producer: it propagates the trace context in
// the message headers and records send spans, metrics and events.
type producer struct {
	port.Producer
	tel  telemetry
	emit func(context.Context, types.BrokerEvent)
}

func (p *producer) SendMessage(ctx context.Context, msg *types.Message) error {
	return p.send(ctx, msg.Topic, []*types.Message{msg}, func(ctx context.Context, out []*types.Message) error {
		return p.Producer.SendMessage(ctx, out[0])
	})
}

func (p *producer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	topic := ""
	if len(msgs) > 0 {
		topic = msgs[0].Topic
		for _, m := range msgs[1:] {
			if m.Topic != topic {
				topic = ""
				break
			}
		}
	}
	return p.send(ctx, topic, msgs, func(ctx context.Context, out []*types.Message) error {
		return p.Producer.SendMessagesBatch(ctx, out)
	})
}

// send injects the trace context into copies of msgs, so the caller's
// messages (possibly shared between goroutines) are never mutated.
func (p *producer) send(ctx context.Context, topic string, msgs []*types.Message, do func(context.Context, []*types.Message) error) error {
	start := time.Now()
	attrs := p.tel.attrs(semconv.MessagingOperationTypeSend, topic)
	spanAttrs := append(attrs, semconv.MessagingOperationName("send"))
	if len(msgs) == 1 {
		spanAttrs = append(spanAttrs, semconv.MessagingMessageID(msgs[0].Id.String()))
	} else {
		spanAttrs = append(spanAttrs, semconv.MessagingBatchMessageCount(len(msgs)))
	}
	name := "send"
	if topic != "" {
		name += " " + topic
	}
	ctx, span := p.tel.tracer().Start(ctx, name, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(spanAttrs...))
	defer span.End()

	out := make([]*types.Message, len(msgs))
	for i, m := range msgs {
		if m == nil {
			out[i] = m
			continue
		}
		cp := *m
		cp.Headers = make(map[string]string, len(m.Headers)+2)
		maps.Copy(cp.Headers, m.Headers)
		inject(ctx, cp.Headers)
		out[i] = &cp
	}

	err := do(ctx, out)
	evType := types.EventMessageSent
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		attrs = append(attrs, semconv.ErrorTypeKey.String(errorType(err)))
		evType = types.EventProducerError
	}
	set := metric.WithAttributeSet(attribute.NewSet(attrs...))
	inst.opDuration.Record(ctx, time.Since(start).Seconds(), set)
	inst.sent.Add(ctx, int64(len(msgs)), set)
	for _, m := range msgs {
		p.emit(ctx, types.BrokerEvent{Type: evType, Topic: m.Topic, MessageID: m.Id.String(), Error: err, Timestamp: time.Now()})
	}
	return err
}

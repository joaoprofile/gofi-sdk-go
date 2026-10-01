package core

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/joaoprofile/gofi-sdk-go/msq/worker"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Dead-letter headers added to the copy published to ConsumeConfig.DeadLetterTopic.
const (
	HeaderDLQOriginalTopic = "x-gofi-dlq-original-topic"
	HeaderDLQError         = "x-gofi-dlq-error"
	HeaderDLQAttempts      = "x-gofi-dlq-attempts"
	// HeaderDLQDeliveries is the broker delivery count of the dead message.
	HeaderDLQDeliveries = "x-gofi-dlq-deliveries"
)

const (
	defaultRetryBackoff = time.Second
	maxRetryBackoff     = 30 * time.Second
)

var inst = newInstruments()

// pipeline runs a handler with recovery, retries, dead-lettering, tracing,
// metrics and events. It is shared by every consumer the service creates.
type pipeline struct {
	cfg     types.ConsumeConfig
	handler port.MessageHandler
	tel     telemetry
	emit    func(context.Context, types.BrokerEvent)
	dlq     port.Producer // nil when DeadLetterTopic is empty
}

// Handle implements port.MessageHandler. ctx stops retry waits; the handler
// itself runs on a context that shutdown does not cancel, so in-flight work
// drains instead of failing mid-way.
//
// Spans and metrics are labeled with the consumer's configured topic, never
// with a value read from the message, which keeps their cardinality bounded.
func (p *pipeline) Handle(ctx context.Context, msg *types.Message) (types.Result, error) {
	start := time.Now()
	destination := p.cfg.Topic
	attrs := p.tel.attrs(semconv.MessagingOperationTypeProcess, destination)

	ctx = extract(ctx, msg.Headers)
	spanAttrs := append(attrs, semconv.MessagingOperationName("process"), semconv.MessagingMessageID(msg.Id.String()))
	if p.cfg.GroupID != "" {
		spanAttrs = append(spanAttrs, semconv.MessagingConsumerGroupName(p.cfg.GroupID))
	}
	if msg.DeliveryCount > 0 {
		spanAttrs = append(spanAttrs, attribute.Int("messaging.gofi.delivery_count", msg.DeliveryCount))
	}
	ctx, span := p.tel.tracer().Start(ctx, "process "+destination,
		trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(spanAttrs...))
	defer span.End()

	p.event(ctx, types.EventMessageReceived, destination, msg, nil)
	result, attempts, err := p.retry(ctx, span, msg)

	if result == types.Nack && ctx.Err() == nil {
		result = p.settleNack(ctx, msg, err, attempts)
	}

	switch result {
	case types.Nack, types.Reject:
		span.SetStatus(codes.Error, resultName(result))
		if err != nil {
			span.RecordError(err)
		}
		attrs = append(attrs, semconv.ErrorTypeKey.String(errorType(err)))
		if result == types.Nack {
			p.event(ctx, types.EventMessageNacked, destination, msg, err)
		} else {
			p.event(ctx, types.EventMessageRejected, destination, msg, err)
		}
	default:
		p.event(ctx, types.EventMessageAcked, destination, msg, nil)
	}
	set := metric.WithAttributeSet(attribute.NewSet(attrs...))
	inst.processDuration.Record(ctx, time.Since(start).Seconds(), set)
	inst.consumed.Add(ctx, 1, set)
	return result, err
}

// settleNack decides the fate of a message the handler nacked: dead-letter it
// (Ack), reject it once the delivery limit is reached, or keep the Nack.
func (p *pipeline) settleNack(ctx context.Context, msg *types.Message, err error, attempts int) types.Result {
	destination := p.cfg.Topic
	switch {
	case p.dlq != nil:
		if dlqErr := p.deadLetter(ctx, msg, err, attempts); dlqErr != nil {
			logging.Error("msq: dead-letter publish failed",
				slog.String("topic", destination), slog.String("dead_letter_topic", p.cfg.DeadLetterTopic), slog.Any("error", dlqErr))
			return types.Nack
		}
		p.event(ctx, types.EventMessageDeadLettered, destination, msg, err)
		return types.Ack
	case p.cfg.DeliveryLimitReached(msg.DeliveryCount):
		// Poison message: a redelivery would fail again. Reject hands it
		// to the broker's own dead-letter route or drops it.
		logging.Error("msq: delivery limit reached, rejecting message",
			slog.String("topic", destination), slog.String("message_id", msg.Id.String()),
			slog.Int("deliveries", msg.DeliveryCount), slog.Any("error", err))
		return types.Reject
	}
	return types.Nack
}

func resultName(r types.Result) string {
	if r == types.Reject {
		return "reject"
	}
	return "nack"
}

// retry runs the handler until it stops returning Nack, MaxRetries is
// exhausted or ctx ends.
func (p *pipeline) retry(ctx context.Context, span trace.Span, msg *types.Message) (types.Result, int, error) {
	backoff := worker.Backoff{Min: p.cfg.RetryBackoff, Max: maxRetryBackoff}
	if backoff.Min <= 0 {
		backoff.Min = defaultRetryBackoff
	}
	backoff.Max = max(backoff.Max, backoff.Min)

	var (
		result types.Result
		err    error
	)
	attempt := 0
	for {
		attempt++
		result, err = p.run(ctx, msg)
		if result != types.Nack || attempt > p.cfg.MaxRetries {
			return result, attempt, err
		}
		span.AddEvent("retry", trace.WithAttributes(attribute.Int("attempt", attempt), attribute.String("error", errorText(err))))
		if worker.Sleep(ctx, backoff.Next()) != nil {
			return result, attempt, err
		}
	}
}

// run calls the handler once, turning a panic into a Nack.
func (p *pipeline) run(ctx context.Context, msg *types.Message) (result types.Result, err error) {
	hctx := context.WithoutCancel(ctx)
	if timeout := p.cfg.EffectiveHandlerTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		hctx, cancel = context.WithTimeout(hctx, timeout)
		defer cancel()
	}
	defer func() {
		if r := recover(); r != nil {
			logging.Error("msq: handler panicked",
				slog.String("topic", p.cfg.Topic), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			result, err = types.Nack, fmt.Errorf("msq: handler panic: %v", r)
		}
	}()
	return p.handler.Handle(hctx, msg)
}

func (p *pipeline) deadLetter(ctx context.Context, msg *types.Message, cause error, attempts int) error {
	// Key is kept: Kafka partitions the copy like the original, and providers
	// route by Topic, never by Key.
	dead := *msg
	dead.Topic = p.cfg.DeadLetterTopic
	dead.DeliveryCount = 0
	dead.Headers = make(map[string]string, len(msg.Headers)+4)
	maps.Copy(dead.Headers, msg.Headers)
	// Providers set msg.Topic from the transport (the real subject or routing
	// key), so it is safe here and more precise than a wildcard subscription.
	dead.Headers[HeaderDLQOriginalTopic] = cmp.Or(msg.Topic, p.cfg.Topic)
	dead.Headers[HeaderDLQError] = truncate(errorText(cause), maxDLQErrorLen)
	dead.Headers[HeaderDLQAttempts] = strconv.Itoa(attempts)
	if msg.DeliveryCount > 0 {
		dead.Headers[HeaderDLQDeliveries] = strconv.Itoa(msg.DeliveryCount)
	}
	return p.dlq.SendMessage(context.WithoutCancel(ctx), &dead)
}

// maxDLQErrorLen bounds the error text copied into a dead-letter header:
// errors may embed payloads, and brokers cap header sizes.
const maxDLQErrorLen = 512

// truncate cuts s to at most n bytes without splitting a UTF-8 rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func (p *pipeline) event(ctx context.Context, t types.BrokerEventType, topic string, msg *types.Message, err error) {
	p.emit(ctx, types.BrokerEvent{Type: t, Topic: topic, MessageID: msg.Id.String(), Error: err, Timestamp: time.Now()})
}

func errorType(err error) string {
	if err == nil {
		return "nack"
	}
	return fmt.Sprintf("%T", err)
}

func errorText(err error) string {
	if err == nil {
		return "nack"
	}
	return err.Error()
}

// consumer decorates a provider consumer with the pipeline.
type consumer struct {
	port.Consumer
	cfg  types.ConsumeConfig
	tel  telemetry
	emit func(context.Context, types.BrokerEvent)
	dlq  port.Producer
}

func (c *consumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	p := &pipeline{cfg: c.cfg, handler: handler, tel: c.tel, emit: c.emit, dlq: c.dlq}
	c.emit(ctx, types.BrokerEvent{Type: types.EventConsumerStarted, Topic: c.cfg.Topic, Timestamp: time.Now()})
	err := c.Consumer.Consume(ctx, p)
	if err != nil {
		c.emit(ctx, types.BrokerEvent{Type: types.EventConsumerError, Topic: c.cfg.Topic, Error: err, Timestamp: time.Now()})
	}
	c.emit(context.WithoutCancel(ctx), types.BrokerEvent{Type: types.EventConsumerStopped, Topic: c.cfg.Topic, Timestamp: time.Now()})
	return err
}

func (c *consumer) Close() error {
	err := c.Consumer.Close()
	if c.dlq != nil {
		if cerr := c.dlq.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

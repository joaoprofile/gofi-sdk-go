package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awssqs "github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/joaoprofile/gofi/base/cloud"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/msq/worker"
	"github.com/joaoprofile/gofi/obs/logging"
)

const (
	maxBatchSize    = 10
	maxReceive      = 10
	waitTimeSeconds = 10
)

// Broker implements port.Broker for AWS SQS.
type Broker struct {
	session *session.Session
}

// New creates a Broker using the default AWS session from the cloud package.
func New() (*Broker, error) {
	sess := cloud.GetSession()
	if sess == nil {
		return nil, errors.New("sqs: AWS session not available")
	}
	s, ok := sess.(*session.Session)
	if !ok {
		return nil, errors.New("sqs: unexpected session type from cloud package")
	}
	return &Broker{session: s}, nil
}

// NewWithSession creates a Broker from an explicit AWS session.
func NewWithSession(sess *session.Session) *Broker {
	return &Broker{session: sess}
}

func (b *Broker) NewProducer() (port.Producer, error) {
	return &sqsProducer{client: awssqs.New(b.session)}, nil
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	return &sqsConsumer{client: awssqs.New(b.session), cfg: cfg, concurrency: concurrency}
}

// Producer

type sqsProducer struct{ client sqsiface.SQSAPI }

func (p *sqsProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	url, err := p.resolveURL(ctx, msg.Topic)
	if err != nil {
		return err
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sqs producer: marshal failed: %w", err)
	}
	_, err = p.client.SendMessageWithContext(ctx, &awssqs.SendMessageInput{
		QueueUrl:    url,
		MessageBody: aws.String(string(body)),
	})
	return err
}

func (p *sqsProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	byTopic := make(map[string][]*types.Message)
	for _, m := range msgs {
		byTopic[m.Topic] = append(byTopic[m.Topic], m)
	}
	for topic, batch := range byTopic {
		url, err := p.resolveURL(ctx, topic)
		if err != nil {
			return err
		}
		for i := 0; i < len(batch); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(batch) {
				end = len(batch)
			}
			if err := p.sendChunk(ctx, url, batch[i:end]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *sqsProducer) sendChunk(ctx context.Context, url *string, msgs []*types.Message) error {
	entries := make([]*awssqs.SendMessageBatchRequestEntry, 0, len(msgs))
	for i, m := range msgs {
		body, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("sqs producer batch: marshal failed: %w", err)
		}
		entries = append(entries, &awssqs.SendMessageBatchRequestEntry{
			Id:          aws.String(fmt.Sprintf("%d", i)),
			MessageBody: aws.String(string(body)),
		})
	}
	out, err := p.client.SendMessageBatchWithContext(ctx, &awssqs.SendMessageBatchInput{
		QueueUrl: url,
		Entries:  entries,
	})
	if err != nil {
		return fmt.Errorf("sqs producer batch: %w", err)
	}
	if len(out.Failed) > 0 {
		return fmt.Errorf("sqs producer batch: %d entries failed", len(out.Failed))
	}
	return nil
}

func (p *sqsProducer) resolveURL(ctx context.Context, name string) (*string, error) {
	out, err := p.client.GetQueueUrlWithContext(ctx, &awssqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return nil, fmt.Errorf("sqs: resolve queue %q failed: %w", name, err)
	}
	return out.QueueUrl, nil
}

func (p *sqsProducer) Close() error { return nil }

// Consumer

type sqsConsumer struct {
	client      sqsiface.SQSAPI
	cfg         types.ConsumeConfig
	concurrency int
	paused      atomic.Bool
}

func (c *sqsConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	url, err := c.resolveURL(ctx, c.cfg.Topic)
	if err != nil {
		return err
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()

	for {
		select {
		case <-ctx.Done():
			logging.Info("sqs consumer: shutting down", slog.String("queue", c.cfg.Topic))
			return nil
		default:
			if c.paused.Load() {
				continue
			}
			c.poll(ctx, url, handler, pool)
		}
	}
}

func (c *sqsConsumer) poll(ctx context.Context, url *string, handler port.MessageHandler, pool *worker.Pool) {
	out, err := c.client.ReceiveMessageWithContext(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:            url,
		MaxNumberOfMessages: aws.Int64(maxReceive),
		WaitTimeSeconds:     aws.Int64(waitTimeSeconds),
	})
	if err != nil {
		logging.Error("sqs consumer: receive failed",
			slog.String("queue", c.cfg.Topic),
			slog.Any("error", err))
		return
	}
	for _, m := range out.Messages {
		m := m
		pool.Enqueue(func() { c.handle(ctx, url, m, handler) })
	}
}

func (c *sqsConsumer) handle(ctx context.Context, url *string, sqsMsg *awssqs.Message, handler port.MessageHandler) {
	var msg types.Message
	body := []byte(aws.StringValue(sqsMsg.Body))
	if err := json.Unmarshal(body, &msg); err != nil || len(msg.Value) == 0 {
		msg = types.Message{Value: body}
	}

	result, err := handler.Handle(ctx, &msg)
	if err != nil {
		logging.Error("sqs consumer: handler error", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
	}

	switch result {
	case types.Ack:
		c.delete(url, sqsMsg)
	case types.Nack:
		// Do not delete — let visibility timeout expire for requeue.
	case types.Ignore:
		// Caller owns the ack decision.
	}
}

func (c *sqsConsumer) delete(url *string, msg *awssqs.Message) {
	_, err := c.client.DeleteMessage(&awssqs.DeleteMessageInput{
		QueueUrl:      url,
		ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		logging.Error("sqs consumer: delete failed", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
	}
}

func (c *sqsConsumer) resolveURL(ctx context.Context, name string) (*string, error) {
	out, err := c.client.GetQueueUrlWithContext(ctx, &awssqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err != nil {
		return nil, fmt.Errorf("sqs consumer: resolve queue %q failed: %w", name, err)
	}
	if out.QueueUrl == nil {
		return nil, fmt.Errorf("sqs consumer: queue %q not found", name)
	}
	return out.QueueUrl, nil
}

func (c *sqsConsumer) Close() error  { return nil }
func (c *sqsConsumer) Pause() error  { c.paused.Store(true); return nil }
func (c *sqsConsumer) Resume() error { c.paused.Store(false); return nil }

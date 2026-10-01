// Package sqs implements the msq broker on Amazon SQS (aws-sdk-go-v2).
package sqs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/joaoprofile/gofi-sdk-go/msq/worker"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

const (
	maxBatchSize    = 10
	maxReceive      = 10
	defaultGroupID  = "default"
	waitTimeSeconds = 10
	deleteTimeout   = 5 * time.Second
	maxVisibility   = 12 * time.Hour // SQS limit
)

// sqsAPI is the subset of the SQS client the broker uses.
type sqsAPI interface {
	GetQueueUrl(ctx context.Context, in *awssqs.GetQueueUrlInput, opts ...func(*awssqs.Options)) (*awssqs.GetQueueUrlOutput, error)
	SendMessage(ctx context.Context, in *awssqs.SendMessageInput, opts ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error)
	SendMessageBatch(ctx context.Context, in *awssqs.SendMessageBatchInput, opts ...func(*awssqs.Options)) (*awssqs.SendMessageBatchOutput, error)
	ReceiveMessage(ctx context.Context, in *awssqs.ReceiveMessageInput, opts ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *awssqs.DeleteMessageInput, opts ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, in *awssqs.ChangeMessageVisibilityInput, opts ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error)
}

// Config selects the AWS identity; the zero value uses the default credential
// chain (IRSA, EKS Pod Identity, instance profile, environment).
type Config struct {
	AWS cloudaws.Config
}

// Broker implements port.Broker for SQS. Queue URLs are resolved once per name.
type Broker struct {
	client sqsAPI
	urls   *queueURLs
}

// New builds a Broker from cfg.
func New(ctx context.Context, cfg Config) (*Broker, error) {
	awsCfg, err := cloudaws.Load(ctx, cfg.AWS)
	if err != nil {
		return nil, fmt.Errorf("sqs: %w", err)
	}
	if awsCfg.Region == "" {
		return nil, errors.New("sqs: region is required (AWS_REGION or Config.AWS.Region)")
	}
	return NewWithConfig(awsCfg), nil
}

// NewWithConfig builds a Broker from an existing aws.Config.
func NewWithConfig(awsCfg awssdk.Config) *Broker {
	return newBroker(awssqs.NewFromConfig(awsCfg))
}

func newBroker(client sqsAPI) *Broker {
	return &Broker{client: client, urls: &queueURLs{client: client}}
}

func (b *Broker) NewProducer() (port.Producer, error) {
	return &sqsProducer{client: b.client, urls: b.urls}, nil
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	lease := min(cmp.Or(cfg.VisibilityTimeout, types.DefaultVisibilityTimeout), maxVisibility)
	return &sqsConsumer{
		client: b.client, urls: b.urls, cfg: cfg, concurrency: concurrency,
		lease: lease, visibility: int32(max(lease.Round(time.Second)/time.Second, 1)), // #nosec G115 -- lease is capped at maxVisibility (12h)
	}, nil
}

// queueURLs caches GetQueueUrl results; URLs never change for a queue name.
type queueURLs struct {
	client sqsAPI
	cache  sync.Map // name -> string
}

func (q *queueURLs) resolve(ctx context.Context, name string) (string, error) {
	if url, ok := q.cache.Load(name); ok {
		return url.(string), nil
	}
	out, err := q.client.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{QueueName: &name})
	if err != nil {
		return "", fmt.Errorf("sqs: resolve queue %q: %w", name, err)
	}
	if out.QueueUrl == nil {
		return "", fmt.Errorf("sqs: queue %q not found", name)
	}
	q.cache.Store(name, *out.QueueUrl)
	return *out.QueueUrl, nil
}

// Producer

type sqsProducer struct {
	client sqsAPI
	urls   *queueURLs
}

func (p *sqsProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	url, err := p.urls.resolve(ctx, msg.Topic)
	if err != nil {
		return err
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sqs producer: marshal: %w", err)
	}
	in := &awssqs.SendMessageInput{QueueUrl: &url, MessageBody: awssdk.String(string(body))}
	if isFIFO(url) {
		in.MessageGroupId, in.MessageDeduplicationId = fifoIDs(msg)
	}
	_, err = p.client.SendMessage(ctx, in)
	return err
}

// isFIFO reports whether the queue is a FIFO queue (name ends in .fifo).
func isFIFO(url string) bool { return strings.HasSuffix(url, ".fifo") }

// fifoIDs orders messages by Key (one group when empty) and deduplicates
// producer retries by Id.
func fifoIDs(m *types.Message) (group, dedup *string) {
	return awssdk.String(cmp.Or(m.Key, defaultGroupID)), awssdk.String(m.Id.String())
}

func (p *sqsProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	byTopic := make(map[string][]*types.Message)
	for _, m := range msgs {
		byTopic[m.Topic] = append(byTopic[m.Topic], m)
	}
	for topic, batch := range byTopic {
		url, err := p.urls.resolve(ctx, topic)
		if err != nil {
			return err
		}
		for i := 0; i < len(batch); i += maxBatchSize {
			if err := p.sendChunk(ctx, url, batch[i:min(i+maxBatchSize, len(batch))]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *sqsProducer) sendChunk(ctx context.Context, url string, msgs []*types.Message) error {
	entries := make([]sqstypes.SendMessageBatchRequestEntry, 0, len(msgs))
	for i, m := range msgs {
		body, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("sqs producer batch: marshal: %w", err)
		}
		entry := sqstypes.SendMessageBatchRequestEntry{
			Id:          awssdk.String(strconv.Itoa(i)),
			MessageBody: awssdk.String(string(body)),
		}
		if isFIFO(url) {
			entry.MessageGroupId, entry.MessageDeduplicationId = fifoIDs(m)
		}
		entries = append(entries, entry)
	}
	out, err := p.client.SendMessageBatch(ctx, &awssqs.SendMessageBatchInput{QueueUrl: &url, Entries: entries})
	if err != nil {
		return fmt.Errorf("sqs producer batch: %w", err)
	}
	if len(out.Failed) > 0 {
		failed := make([]string, 0, len(out.Failed))
		for _, f := range out.Failed {
			failed = append(failed, awssdk.ToString(f.Id)+": "+awssdk.ToString(f.Message))
		}
		return fmt.Errorf("sqs producer batch: %d entries failed: %s", len(out.Failed), strings.Join(failed, "; "))
	}
	return nil
}

func (p *sqsProducer) Close() error { return nil }

// Consumer

type sqsConsumer struct {
	client      sqsAPI
	urls        *queueURLs
	cfg         types.ConsumeConfig
	concurrency int
	gate        worker.Gate
	lease       time.Duration // visibility granted on receive and on every renewal
	visibility  int32         // lease in seconds
}

func (c *sqsConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	url, err := c.urls.resolve(ctx, c.cfg.Topic)
	if err != nil {
		return err
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()
	slots := worker.NewSlots(c.concurrency)

	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		// Blocks while paused; returns once ctx is done.
		if c.gate.Wait(ctx) != nil || ctx.Err() != nil {
			logging.Info("sqs consumer: shutting down", slog.String("queue", c.cfg.Topic))
			return nil
		}
		// Receive only what can start now: a buffered message's visibility
		// would run out before its handler even starts.
		free, err := slots.Acquire(ctx, maxReceive)
		if err != nil {
			continue
		}
		if err := c.poll(ctx, url, handler, pool, slots, free); err != nil {
			_ = worker.Sleep(ctx, backoff.Next())
			continue
		}
		backoff.Reset()
	}
}

// poll receives up to free messages, each holding one of the acquired slots
// until its handler finishes; unused slots are released at once.
func (c *sqsConsumer) poll(ctx context.Context, url string, handler port.MessageHandler, pool *worker.Pool, slots *worker.Slots, free int) error {
	in := &awssqs.ReceiveMessageInput{
		QueueUrl:            &url,
		MaxNumberOfMessages: int32(free), // #nosec G115 -- free is at most maxReceive (10)
		WaitTimeSeconds:     waitTimeSeconds,
		VisibilityTimeout:   c.visibility,
	}
	in.MessageSystemAttributeNames = []sqstypes.MessageSystemAttributeName{sqstypes.MessageSystemAttributeNameApproximateReceiveCount}
	fifo := isFIFO(url)
	if fifo {
		in.MessageSystemAttributeNames = append(in.MessageSystemAttributeNames, sqstypes.MessageSystemAttributeNameMessageGroupId)
	}
	out, err := c.client.ReceiveMessage(ctx, in)
	if err != nil {
		slots.Release(free)
		if ctx.Err() == nil {
			logging.Error("sqs consumer: receive failed", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
		}
		return err
	}
	slots.Release(free - len(out.Messages))
	// Leases are renewed from receipt: FIFO messages wait for their group.
	stops := make(map[*sqstypes.Message]func(), len(out.Messages))
	for i := range out.Messages {
		stops[&out.Messages[i]] = c.keepAlive(ctx, url, out.Messages[i])
	}
	run := func(m *sqstypes.Message) {
		defer slots.Release(1)
		c.handle(ctx, url, *m, handler, stops[m])
	}
	if !fifo {
		for i := range out.Messages {
			pool.Enqueue(func() { run(&out.Messages[i]) })
		}
		return nil
	}
	// FIFO: groups run in parallel, messages of one group in order. SQS holds
	// back the rest of a group while these are in flight.
	var order []string
	groups := make(map[string][]*sqstypes.Message)
	for i := range out.Messages {
		m := &out.Messages[i]
		g := m.Attributes[string(sqstypes.MessageSystemAttributeNameMessageGroupId)]
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], m)
	}
	for _, g := range order {
		msgs := groups[g]
		pool.Enqueue(func() {
			for _, m := range msgs {
				run(m)
			}
		})
	}
	return nil
}

// keepAlive renews the message's visibility until the returned stop is called.
func (c *sqsConsumer) keepAlive(ctx context.Context, url string, m sqstypes.Message) func() {
	return worker.KeepAlive(ctx, worker.RenewEvery(c.lease), func(rctx context.Context) {
		if _, err := c.client.ChangeMessageVisibility(rctx, &awssqs.ChangeMessageVisibilityInput{
			QueueUrl: &url, ReceiptHandle: m.ReceiptHandle, VisibilityTimeout: c.visibility,
		}); err != nil {
			logging.Warn("sqs consumer: visibility renewal failed", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
		}
	})
}

// handle stops the lease renewal before settling, so a renewal never races
// the delete.
func (c *sqsConsumer) handle(ctx context.Context, url string, sqsMsg sqstypes.Message, handler port.MessageHandler, stopLease func()) {
	msg := decode(c.cfg.Topic, sqsMsg)

	result, err := handler.Handle(ctx, &msg)
	stopLease()
	if err != nil {
		logging.Error("sqs consumer: handler error", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
	}
	switch result {
	case types.Nack:
		// Not deleted: SQS redelivers once the last granted visibility ends
		// (or its redrive policy moves it to the queue's DLQ).
	default:
		// Ack, Ignore and Reject (delivery limit reached) remove it.
		c.delete(ctx, url, sqsMsg)
	}
}

// decode sets Topic to the queue consumed and DeliveryCount from
// ApproximateReceiveCount; a message without an Id gets its SQS MessageId,
// which is the same on every receive.
func decode(queue string, sm sqstypes.Message) types.Message {
	msg := types.DecodeEnvelope([]byte(awssdk.ToString(sm.Body)))
	msg.Topic = queue
	if n, err := strconv.Atoi(sm.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)]); err == nil {
		msg.DeliveryCount = n
	}
	if msg.Id == uuid.Nil {
		if id := awssdk.ToString(sm.MessageId); id != "" {
			msg.Id = types.StableID(id)
		} else {
			msg.Id = uuid.New()
		}
	}
	return msg
}

// delete survives shutdown cancellation so processed messages are not redelivered.
func (c *sqsConsumer) delete(ctx context.Context, url string, msg sqstypes.Message) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteTimeout)
	defer cancel()
	if _, err := c.client.DeleteMessage(dctx, &awssqs.DeleteMessageInput{QueueUrl: &url, ReceiptHandle: msg.ReceiptHandle}); err != nil {
		logging.Error("sqs consumer: delete failed", slog.String("queue", c.cfg.Topic), slog.Any("error", err))
	}
}

func (c *sqsConsumer) Close() error  { return nil }
func (c *sqsConsumer) Pause() error  { c.gate.Pause(); return nil }
func (c *sqsConsumer) Resume() error { c.gate.Resume(); return nil }

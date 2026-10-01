package oci

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
	"github.com/oracle/oci-go-sdk/v65/queue"
)

// queueClientAPI abstracts *queue.QueueClient so that it can be mocked in tests.
type queueClientAPI interface {
	PutMessages(ctx context.Context, req queue.PutMessagesRequest) (queue.PutMessagesResponse, error)
	GetMessages(ctx context.Context, req queue.GetMessagesRequest) (queue.GetMessagesResponse, error)
	DeleteMessage(ctx context.Context, req queue.DeleteMessageRequest) (queue.DeleteMessageResponse, error)
	UpdateMessage(ctx context.Context, req queue.UpdateMessageRequest) (queue.UpdateMessageResponse, error)
}

const (
	deleteTimeout = 5 * time.Second // bounds the delete issued after a handler finishes
	maxReceive    = 20              // GetMessages limit
	maxVisibility = 12 * time.Hour  // OCI Queue limit
)

// Config configures the OCI Queue broker.
type Config struct {
	// Credentials selects the principal (API key, instance, resource or
	// workload identity) and region, shared with other OCI integrations.
	Credentials cloudoci.Config
	// QueueURL is the queue's messages endpoint; it defaults to the
	// region's cell-1 endpoint.
	QueueURL string
}

// Broker implements port.Broker for OCI Queue.
type Broker struct {
	client queueClientAPI
}

// New creates a Broker from the given configuration.
func New(cfg Config) (*Broker, error) {
	if cfg.Credentials.Region == "" {
		return nil, fmt.Errorf("oci queue: region is required")
	}
	provider, err := cloudoci.ConfigurationProvider(cfg.Credentials)
	if err != nil {
		return nil, fmt.Errorf("oci queue: %w", err)
	}
	client, err := queue.NewQueueClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("oci queue: create client: %w", err)
	}
	client.Host = cfg.QueueURL
	if client.Host == "" {
		client.Host = fmt.Sprintf("https://cell-1.queue.messaging.%s.oci.oraclecloud.com", cfg.Credentials.Region)
	}
	return &Broker{client: &client}, nil
}

func (b *Broker) NewProducer() (port.Producer, error) {
	return &ociProducer{client: b.client}, nil
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	return newConsumer(b.client, cfg, concurrency), nil
}

func newConsumer(client queueClientAPI, cfg types.ConsumeConfig, concurrency int) *ociConsumer {
	lease := min(cmp.Or(cfg.VisibilityTimeout, types.DefaultVisibilityTimeout), maxVisibility)
	return &ociConsumer{
		client: client, cfg: cfg, concurrency: concurrency,
		lease: lease, visibility: int(max(lease.Round(time.Second)/time.Second, 1)),
	}
}

// Producer

type ociProducer struct{ client queueClientAPI }

func (p *ociProducer) SendMessage(ctx context.Context, msg *types.Message) error {
	if msg.Topic == "" {
		return fmt.Errorf("oci producer: msg.Topic (queue OCID) must be set")
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("oci producer: marshal failed: %w", err)
	}
	content := string(body)
	resp, err := p.client.PutMessages(ctx, queue.PutMessagesRequest{
		QueueId: &msg.Topic,
		PutMessagesDetails: queue.PutMessagesDetails{
			Messages: []queue.PutMessagesDetailsEntry{{Content: &content}},
		},
	})
	if err != nil {
		return fmt.Errorf("oci producer: put failed: %w", err)
	}
	if len(resp.Messages) == 0 {
		return fmt.Errorf("oci producer: no message confirmation received")
	}
	return nil
}

func (p *ociProducer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	byQueue := make(map[string][]*types.Message)
	for _, m := range msgs {
		byQueue[m.Topic] = append(byQueue[m.Topic], m)
	}
	for queueID, batch := range byQueue {
		entries := make([]queue.PutMessagesDetailsEntry, 0, len(batch))
		for _, m := range batch {
			body, err := json.Marshal(m)
			if err != nil {
				return fmt.Errorf("oci producer batch: marshal failed: %w", err)
			}
			content := string(body)
			entries = append(entries, queue.PutMessagesDetailsEntry{Content: &content})
		}
		qid := queueID
		resp, err := p.client.PutMessages(ctx, queue.PutMessagesRequest{
			QueueId:            &qid,
			PutMessagesDetails: queue.PutMessagesDetails{Messages: entries},
		})
		if err != nil {
			return fmt.Errorf("oci producer batch: %w", err)
		}
		if len(resp.Messages) != len(entries) {
			return fmt.Errorf("oci producer batch: expected %d confirmations, got %d", len(entries), len(resp.Messages))
		}
	}
	return nil
}

func (p *ociProducer) Close() error { return nil }

// Consumer

type ociConsumer struct {
	client      queueClientAPI
	cfg         types.ConsumeConfig
	concurrency int
	gate        worker.Gate
	lease       time.Duration // visibility granted on receive and on every renewal
	visibility  int           // lease in seconds
}

func (c *ociConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	if c.cfg.QueueID == "" {
		return fmt.Errorf("oci consumer: ConsumeConfig.QueueID must be set")
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()
	slots := worker.NewSlots(c.concurrency)

	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		// Blocks while paused; returns once ctx is done.
		if c.gate.Wait(ctx) != nil || ctx.Err() != nil {
			logging.Info("oci consumer: shutting down", slog.String("queue_id", c.cfg.QueueID))
			return nil
		}
		// Receive only what can start now: a buffered message's visibility
		// would run out before its handler even starts.
		free, err := slots.Acquire(ctx, maxReceive)
		if err != nil {
			continue
		}
		if err := c.poll(ctx, handler, pool, slots, free); err != nil {
			_ = worker.Sleep(ctx, backoff.Next())
			continue
		}
		backoff.Reset()
	}
}

// poll receives up to free messages, each holding one of the acquired slots
// until its handler finishes; unused slots are released at once.
func (c *ociConsumer) poll(ctx context.Context, handler port.MessageHandler, pool *worker.Pool, slots *worker.Slots, free int) error {
	resp, err := c.client.GetMessages(ctx, queue.GetMessagesRequest{
		QueueId:             &c.cfg.QueueID,
		VisibilityInSeconds: new(c.visibility),
		Limit:               new(free),
	})
	if err != nil {
		slots.Release(free)
		if ctx.Err() == nil {
			logging.Error("oci consumer: get messages failed",
				slog.String("queue_id", c.cfg.QueueID),
				slog.Any("error", err))
		}
		return err
	}
	slots.Release(free - len(resp.Messages))
	for _, m := range resp.Messages {
		stop := c.keepAlive(ctx, m)
		pool.Enqueue(func() {
			defer slots.Release(1)
			c.handle(ctx, m, handler, stop)
		})
	}
	return nil
}

// keepAlive renews the message's visibility until the returned stop is called.
func (c *ociConsumer) keepAlive(ctx context.Context, m queue.GetMessage) func() {
	if m.Receipt == nil {
		return func() {}
	}
	return worker.KeepAlive(ctx, worker.RenewEvery(c.lease), func(rctx context.Context) {
		if _, err := c.client.UpdateMessage(rctx, queue.UpdateMessageRequest{
			QueueId:              &c.cfg.QueueID,
			MessageReceipt:       m.Receipt,
			UpdateMessageDetails: queue.UpdateMessageDetails{VisibilityInSeconds: new(c.visibility)},
		}); err != nil {
			logging.Warn("oci consumer: visibility renewal failed", slog.String("queue_id", c.cfg.QueueID), slog.Any("error", err))
		}
	})
}

// handle stops the lease renewal before settling, so a renewal never races
// the delete.
func (c *ociConsumer) handle(ctx context.Context, ociMsg queue.GetMessage, handler port.MessageHandler, stopLease func()) {
	msg := c.decode(ociMsg)

	result, err := handler.Handle(ctx, &msg)
	stopLease()
	if err != nil {
		logging.Error("oci consumer: handler error", slog.String("queue_id", c.cfg.QueueID), slog.Any("error", err))
	}

	switch result {
	case types.Ack, types.Ignore, types.Reject:
		// Ack = processed, Ignore = discarded on purpose, Reject = delivery
		// limit reached. All remove the message: on a visibility-timeout
		// queue, not deleting IS a requeue.
		c.delete(ctx, ociMsg)
	case types.Nack:
		// Visible again once the last granted visibility ends.
	}
}

// decode sets Topic to the queue OCID consumed and DeliveryCount from the
// queue's own counter; a message without an Id gets one from the OCI message
// id, which is the same on every delivery.
func (c *ociConsumer) decode(m queue.GetMessage) types.Message {
	content := ""
	if m.Content != nil {
		content = *m.Content
	}
	msg := types.DecodeEnvelope([]byte(content))
	msg.Topic = c.cfg.QueueID
	if m.DeliveryCount != nil {
		msg.DeliveryCount = *m.DeliveryCount
	}
	if msg.Id == uuid.Nil {
		if m.Id != nil {
			msg.Id = types.StableID(fmt.Sprintf("oci/%s/%d", c.cfg.QueueID, *m.Id))
		} else {
			msg.Id = uuid.New()
		}
	}
	return msg
}

// delete survives shutdown cancellation so processed messages are not redelivered.
func (c *ociConsumer) delete(ctx context.Context, m queue.GetMessage) {
	if m.Receipt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteTimeout)
	defer cancel()
	if _, err := c.client.DeleteMessage(ctx, queue.DeleteMessageRequest{
		QueueId:        &c.cfg.QueueID,
		MessageReceipt: m.Receipt,
	}); err != nil {
		logging.Error("oci consumer: delete failed", slog.String("queue_id", c.cfg.QueueID), slog.Any("error", err))
	}
}

func (c *ociConsumer) Close() error  { return nil }
func (c *ociConsumer) Pause() error  { c.gate.Pause(); return nil }
func (c *ociConsumer) Resume() error { c.gate.Resume(); return nil }

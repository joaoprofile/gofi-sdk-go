package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/msq/worker"
	"github.com/joaoprofile/gofi/obs/logging"
	ocicommon "github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/queue"
)

const defaultQueueURL = "https://cell-1.queue.messaging.sa-saopaulo-1.oci.oraclecloud.com"

// queueClientAPI abstracts *queue.QueueClient so that it can be mocked in tests.
type queueClientAPI interface {
	PutMessages(ctx context.Context, req queue.PutMessagesRequest) (queue.PutMessagesResponse, error)
	GetMessages(ctx context.Context, req queue.GetMessagesRequest) (queue.GetMessagesResponse, error)
	DeleteMessage(ctx context.Context, req queue.DeleteMessageRequest) (queue.DeleteMessageResponse, error)
}

// Config configures the OCI Queue broker.
type Config struct {
	TenancyID   string
	UserID      string
	Region      string
	FingerPrint string
	PrivateKey  string
	QueueURL    string // defaults to São Paulo region endpoint
}

// Broker implements port.Broker for OCI Queue.
type Broker struct {
	client queueClientAPI
}

// New creates a Broker from the given configuration.
func New(cfg Config) (*Broker, error) {
	if cfg.TenancyID == "" || cfg.UserID == "" || cfg.Region == "" || cfg.FingerPrint == "" {
		return nil, fmt.Errorf("oci: missing required credentials (TenancyID, UserID, Region, FingerPrint)")
	}

	provider := ocicommon.NewRawConfigurationProvider(
		cfg.TenancyID, cfg.UserID, cfg.Region, cfg.FingerPrint, cfg.PrivateKey, nil,
	)

	client, err := queue.NewQueueClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("oci: failed to create queue client: %w", err)
	}

	queueURL := cfg.QueueURL
	if queueURL == "" {
		queueURL = defaultQueueURL
	}
	client.Host = queueURL

	return &Broker{client: &client}, nil
}

func (b *Broker) NewProducer() (port.Producer, error) {
	return &ociProducer{client: b.client}, nil
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = types.DefaultConcurrency
	}
	return &ociConsumer{client: b.client, cfg: cfg, concurrency: concurrency}
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
	paused      atomic.Bool
}

func (c *ociConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	if c.cfg.QueueID == "" {
		return fmt.Errorf("oci consumer: ConsumeConfig.QueueID must be set")
	}

	pool := worker.New(c.concurrency)
	defer pool.Close()

	visibilitySec := int(c.cfg.PollInterval.Seconds())
	if visibilitySec <= 0 {
		visibilitySec = int(types.DefaultPollInterval.Seconds())
	}

	for {
		select {
		case <-ctx.Done():
			logging.Info("oci consumer: shutting down", slog.String("queue_id", c.cfg.QueueID))
			return nil
		default:
			if c.paused.Load() {
				continue
			}
			c.poll(ctx, handler, pool, visibilitySec)
		}
	}
}

func (c *ociConsumer) poll(ctx context.Context, handler port.MessageHandler, pool *worker.Pool, visibilitySec int) {
	resp, err := c.client.GetMessages(ctx, queue.GetMessagesRequest{
		QueueId:             &c.cfg.QueueID,
		VisibilityInSeconds: ocicommon.Int(visibilitySec),
		Limit:               ocicommon.Int(c.concurrency),
	})
	if err != nil {
		logging.Error("oci consumer: get messages failed",
			slog.String("queue_id", c.cfg.QueueID),
			slog.Any("error", err))
		return
	}
	for _, m := range resp.Messages {
		m := m
		pool.Enqueue(func() { c.handle(ctx, m, handler) })
	}
}

func (c *ociConsumer) handle(ctx context.Context, ociMsg queue.GetMessage, handler port.MessageHandler) {
	content := ""
	if ociMsg.Content != nil {
		content = *ociMsg.Content
	}
	var msg types.Message
	// Valid JSON that is not a types.Message envelope unmarshals into all-zero
	// fields — deliver the raw body instead of an empty Value.
	if err := json.Unmarshal([]byte(content), &msg); err != nil || len(msg.Value) == 0 {
		msg = types.Message{Value: []byte(content)}
	}

	result, err := handler.Handle(ctx, &msg)
	if err != nil {
		logging.Error("oci consumer: handler error", slog.String("queue_id", c.cfg.QueueID), slog.Any("error", err))
	}

	switch result {
	case types.Ack, types.Ignore:
		// Ack = processed, Ignore = discarded on purpose. Both remove the
		// message: on a visibility-timeout queue, not deleting IS a requeue.
		c.delete(ctx, ociMsg)
	case types.Nack:
		// Visibility timeout will expire and message becomes visible again.
	}
}

func (c *ociConsumer) delete(ctx context.Context, m queue.GetMessage) {
	if m.Receipt == nil {
		return
	}
	if _, err := c.client.DeleteMessage(ctx, queue.DeleteMessageRequest{
		QueueId:        &c.cfg.QueueID,
		MessageReceipt: m.Receipt,
	}); err != nil {
		logging.Error("oci consumer: delete failed", slog.String("queue_id", c.cfg.QueueID), slog.Any("error", err))
	}
}

func (c *ociConsumer) Close() error  { return nil }
func (c *ociConsumer) Pause() error  { c.paused.Store(true); return nil }
func (c *ociConsumer) Resume() error { c.paused.Store(false); return nil }

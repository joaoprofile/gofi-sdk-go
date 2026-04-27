// Package kafka implements port.Broker for Apache Kafka using the Sarama library.
package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
)

// TopicConfig describes a Kafka topic that should be created during Setup.
type TopicConfig struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	ConfigEntries     map[string]*string
}

// Config configures the Kafka broker.
type Config struct {
	Brokers  []string
	User     string
	Password string
	UseTLS   bool
	ClientID string
	// Topics lists topics to be created idempotently when Setup is called.
	Topics []TopicConfig
}

// clusterAdmin abstracts sarama.ClusterAdmin to allow injection of test doubles.
type clusterAdmin interface {
	CreateTopic(topic string, detail *sarama.TopicDetail, validateOnly bool) error
	Close() error
}

// ConfigFromEnv builds a Config from environment variables.
func ConfigFromEnv() Config {
	env := environment.Instance()
	return Config{
		Brokers:  []string{fmt.Sprintf("%s:%d", env.MessagingHost, env.MessagingPort)},
		User:     env.MessagingUser,
		Password: env.MessagingPassword,
	}
}

// Broker implements port.Broker for Kafka.
type Broker struct {
	brokers      []string
	config       *sarama.Config
	topics       []TopicConfig
	adminFactory func(brokers []string, cfg *sarama.Config) (clusterAdmin, error)
}

// New creates a Broker from the given configuration.
func New(cfg Config) (*Broker, error) {
	sc := sarama.NewConfig()
	sc.Producer.Return.Successes = true
	sc.Producer.Partitioner = sarama.NewHashPartitioner
	sc.Version = sarama.V2_8_0_0
	if cfg.ClientID != "" {
		sc.ClientID = cfg.ClientID
	}
	if cfg.User != "" && cfg.Password != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = cfg.User
		sc.Net.SASL.Password = cfg.Password
		sc.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		sc.Net.TLS.Enable = cfg.UseTLS
	}
	return &Broker{
		brokers:      cfg.Brokers,
		config:       sc,
		topics:       cfg.Topics,
		adminFactory: defaultAdminFactory,
	}, nil
}

func defaultAdminFactory(brokers []string, cfg *sarama.Config) (clusterAdmin, error) {
	return sarama.NewClusterAdmin(brokers, cfg)
}

// Setup implements port.BrokerSetup. It creates the configured topics
// idempotently — topics that already exist are silently skipped.
func (b *Broker) Setup(_ context.Context) error {
	if len(b.topics) == 0 {
		return nil
	}

	admin, err := b.adminFactory(b.brokers, b.config)
	if err != nil {
		return fmt.Errorf("kafka setup: create admin: %w", err)
	}
	defer admin.Close() //nolint:errcheck

	for _, t := range b.topics {
		partitions := t.Partitions
		if partitions <= 0 {
			partitions = 1
		}
		replication := t.ReplicationFactor
		if replication <= 0 {
			replication = 1
		}
		detail := &sarama.TopicDetail{
			NumPartitions:     partitions,
			ReplicationFactor: replication,
			ConfigEntries:     t.ConfigEntries,
		}
		if err := admin.CreateTopic(t.Name, detail, false); err != nil {
			if kerr, ok := err.(*sarama.TopicError); ok && kerr.Err == sarama.ErrTopicAlreadyExists {
				logging.Info("kafka: topic already exists", slog.String("topic", t.Name))
				continue
			}
			return fmt.Errorf("kafka setup: create topic %q: %w", t.Name, err)
		}
		logging.Info("kafka: topic created", slog.String("topic", t.Name))
	}
	return nil
}

func (b *Broker) NewProducer() port.Producer {
	prod, err := sarama.NewSyncProducer(b.brokers, b.config)
	if err != nil {
		logging.Error("kafka: failed to create producer", slog.Any("error", err))
		return nil
	}
	return &kafkaProducer{producer: prod}
}

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	groupID := cfg.GroupID
	if groupID == "" {
		groupID = cfg.Topic
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	group, err := sarama.NewConsumerGroup(b.brokers, groupID, b.config)
	if err != nil {
		logging.Error("kafka: failed to create consumer group",
			slog.String("topic", cfg.Topic),
			slog.Any("error", err))
		return nil
	}
	return &kafkaConsumer{group: group, cfg: cfg, concurrency: concurrency}
}

// Producer

type kafkaProducer struct{ producer sarama.SyncProducer }

func (p *kafkaProducer) SendMessage(_ context.Context, msg *types.Message) error {
	_, _, err := p.producer.SendMessage(&sarama.ProducerMessage{
		Topic:   msg.Topic,
		Key:     sarama.StringEncoder(msg.Key),
		Value:   sarama.ByteEncoder(msg.Value),
		Headers: toRecordHeaders(msg.Headers),
	})
	return err
}

func (p *kafkaProducer) SendMessagesBatch(_ context.Context, msgs []*types.Message) error {
	batch := make([]*sarama.ProducerMessage, 0, len(msgs))
	for _, m := range msgs {
		batch = append(batch, &sarama.ProducerMessage{
			Topic:   m.Topic,
			Key:     sarama.StringEncoder(m.Key),
			Value:   sarama.ByteEncoder(m.Value),
			Headers: toRecordHeaders(m.Headers),
		})
	}
	return p.producer.SendMessages(batch)
}

// toRecordHeaders converts the transport-agnostic string map into the sarama
// record-header slice. Returns nil when there are no headers so the producer
// does not allocate an empty slice on every send.
func toRecordHeaders(headers map[string]string) []sarama.RecordHeader {
	if len(headers) == 0 {
		return nil
	}
	out := make([]sarama.RecordHeader, 0, len(headers))
	for k, v := range headers {
		out = append(out, sarama.RecordHeader{Key: []byte(k), Value: []byte(v)})
	}
	return out
}

// fromRecordHeaders flattens sarama record headers back into a string map so
// handlers can read metadata via types.Message.Headers regardless of broker.
func fromRecordHeaders(headers []*sarama.RecordHeader) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, h := range headers {
		if h == nil {
			continue
		}
		out[string(h.Key)] = string(h.Value)
	}
	return out
}

func (p *kafkaProducer) Close() error { return p.producer.Close() }

// Consumer

type kafkaConsumer struct {
	group       sarama.ConsumerGroup
	cfg         types.ConsumeConfig
	concurrency int
}

func (c *kafkaConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	topics := []string{c.cfg.Topic}
	var wg sync.WaitGroup
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := &groupHandler{handler: handler, cfg: c.cfg}
			for {
				if err := c.group.Consume(ctx, topics, h); err != nil {
					logging.Error("kafka consumer: session error",
						slog.String("topic", c.cfg.Topic),
						slog.Any("error", err))
				}
				if ctx.Err() != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
	return nil
}

func (c *kafkaConsumer) Close() error  { return c.group.Close() }
func (c *kafkaConsumer) Pause() error  { return nil }
func (c *kafkaConsumer) Resume() error { return nil }

// groupHandler implements sarama.ConsumerGroupHandler.
type groupHandler struct {
	handler port.MessageHandler
	cfg     types.ConsumeConfig
}

func (h *groupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *groupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for sm := range claim.Messages() {
		msg := &types.Message{
			Id:        uuid.New(),
			Topic:     sm.Topic,
			Key:       string(sm.Key),
			Value:     sm.Value,
			Timestamp: sm.Timestamp,
			Headers:   fromRecordHeaders(sm.Headers),
		}

		var lastErr error
		for attempt := 0; attempt <= h.cfg.MaxRetries; attempt++ {
			if attempt > 0 {
				backoff := h.cfg.RetryBackoff
				if backoff <= 0 {
					backoff = time.Second
				}
				time.Sleep(backoff)
			}
			result, err := h.handler.Handle(context.Background(), msg)
			lastErr = err
			if result == types.Ack || result == types.Ignore || err == nil {
				lastErr = nil
				break
			}
		}

		if lastErr != nil && h.cfg.DeadLetterTopic != "" {
			logging.Error("kafka consumer: exhausted retries, message lost (DLQ not yet wired)",
				slog.String("topic", h.cfg.Topic),
				slog.String("dlq", h.cfg.DeadLetterTopic))
		}

		if !h.cfg.AutoCommit {
			session.MarkMessage(sm, "")
		}
	}
	return nil
}

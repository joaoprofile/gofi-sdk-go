// Package kafka implements port.Broker for Apache Kafka using the Sarama library.
package kafka

import (
	"context"
	"crypto/tls"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
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
	// SASLMechanism selects the SASL mechanism: "PLAIN" (default),
	// "SCRAM-SHA-256" or "SCRAM-SHA-512". Managed brokers such as OCI Kafka
	// typically require SCRAM. Empty means PLAIN.
	SASLMechanism string
	ClientID      string
	// Topics lists topics to be created idempotently when Setup is called.
	Topics []TopicConfig
}

// clusterAdmin abstracts sarama.ClusterAdmin to allow injection of test doubles.
type clusterAdmin interface {
	CreateTopic(topic string, detail *sarama.TopicDetail, validateOnly bool) error
	Close() error
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
	mechanism := "PLAIN"
	if cfg.User != "" && cfg.Password != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.User = cfg.User
		sc.Net.SASL.Password = cfg.Password
		switch strings.ToUpper(strings.TrimSpace(cfg.SASLMechanism)) {
		case "SCRAM-SHA-256":
			mechanism = "SCRAM-SHA-256"
			sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{HashGeneratorFcn: sha256GeneratorFcn}
			}
		case "SCRAM-SHA-512":
			mechanism = "SCRAM-SHA-512"
			sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &scramClient{HashGeneratorFcn: sha512GeneratorFcn}
			}
		default:
			sc.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		}
	}
	// TLS is independent of SASL: managed brokers commonly require SASL_SSL,
	// but plain TLS (no auth) is also valid. Enable via MESSAGING_USE_TLS.
	if cfg.UseTLS {
		sc.Net.TLS.Enable = true
		sc.Net.TLS.Config = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	// Startup diagnostics: make the resolved target explicit so connection
	// failures ("run out of available brokers") are not opaque. Set
	// MESSAGING_DEBUG=true to surface Sarama's per-broker connection errors.
	logging.Info("kafka: broker config",
		slog.Any("brokers", cfg.Brokers),
		slog.Bool("tls", cfg.UseTLS),
		slog.Bool("sasl", sc.Net.SASL.Enable),
		slog.String("mechanism", mechanism),
	)
	if enableSaramaDebug() {
		sarama.Logger = stdlog.New(os.Stderr, "[sarama] ", stdlog.LstdFlags)
	}

	return &Broker{
		brokers:      cfg.Brokers,
		config:       sc,
		topics:       cfg.Topics,
		adminFactory: defaultAdminFactory,
	}, nil
}

func enableSaramaDebug() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MESSAGING_DEBUG")))
	return v == "1" || v == "true" || v == "yes"
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

func (b *Broker) NewProducer() (port.Producer, error) {
	prod, err := sarama.NewSyncProducer(b.brokers, b.config)
	if err != nil {
		logging.Error("kafka: failed to create producer", slog.Any("error", err))
		return nil, fmt.Errorf("kafka: create producer: %w", err)
	}
	return &kafkaProducer{producer: prod}, nil
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

	sc := *b.config
	switch cfg.InitialOffset {
	case types.OffsetResetEarliest:
		sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	case types.OffsetResetLatest:
		sc.Consumer.Offsets.Initial = sarama.OffsetNewest
	}

	brokers := b.brokers
	return &kafkaConsumer{
		cfg:         cfg,
		concurrency: concurrency,
		newGroup: func() (sarama.ConsumerGroup, error) {
			return sarama.NewConsumerGroup(brokers, groupID, &sc)
		},
	}
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
	cfg         types.ConsumeConfig
	concurrency int
	newGroup    func() (sarama.ConsumerGroup, error)
}

// Consume sobe `concurrency` workers, cada um com seu PRÓPRIO sarama
// ConsumerGroup. Cada ConsumerGroup é um MEMBRO distinto no group do Kafka, então
// as partições do tópico se distribuem entre eles (paralelismo real até
// min(concurrency, partições)).
//
// Por que não 1 ConsumerGroup compartilhado por N goroutines: um ConsumerGroup
// tem um único memberID; N goroutines chamando Consume() no mesmo group
// re-entram no JoinGroup e invalidam a generation umas das outras → erro
// "member not known in the current generation" em loop (rebalance churn). 1
// group por worker elimina isso.
func (c *kafkaConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	topics := []string{c.cfg.Topic}
	var wg sync.WaitGroup
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			group, err := c.newGroup()
			if err != nil {
				logging.Error("kafka consumer: failed to create consumer group",
					slog.String("topic", c.cfg.Topic),
					slog.String("group_id", c.cfg.GroupID),
					slog.Any("error", err))
				return
			}
			defer group.Close()

			h := &groupHandler{handler: handler, cfg: c.cfg}
			for {
				if err := group.Consume(ctx, topics, h); err != nil {
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

// Close é no-op: cada worker fecha o próprio ConsumerGroup via defer quando
// Consume retorna (ctx cancelado pelo ConsumerManager). Mantido pra satisfazer
// port.Consumer.
func (c *kafkaConsumer) Close() error  { return nil }
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
	// Sinal de ofuscação de offset: start NEGATIVO (sentinel newest/oldest) =
	// Sarama não tinha offset commitado utilizável (ausente OU out-of-range por
	// retenção ter apagado além do commit) e caiu na política. Só importa quando
	// HÁ dado no tópico (hwm>0): newest pulando backlog real é o ponto cego que
	// esconde "não consome" atrás de um offset stale. WARN só nesse caso — group
	// novo em tópico vazio (hwm==0) é normal e fica silencioso.
	if start := claim.InitialOffset(); start == sarama.OffsetNewest && claim.HighWaterMarkOffset() > 0 {
		logging.Warn("kafka consumer: claim começou no FIM — sem offset commitado válido, backlog existente PULADO",
			slog.String("topic", claim.Topic()),
			slog.String("group_id", h.cfg.GroupID),
			slog.Int("partition", int(claim.Partition())),
			slog.Int64("high_water_mark", claim.HighWaterMarkOffset()))
	}

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

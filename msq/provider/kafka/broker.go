// Package kafka implements port.Broker for Apache Kafka using the Sarama library.
package kafka

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
)

// TopicConfig describes a Kafka topic that should be created during Setup.
type TopicConfig struct {
	Name              string
	Partitions        int32
	ReplicationFactor int16
	ConfigEntries     map[string]*string
}

// Config configures the Kafka broker. Printing or logging it redacts the
// password.
type Config struct {
	Brokers []string
	// User and Password enable SASL; set both or neither.
	User     string
	Password string
	UseTLS   bool
	// TLS is the client TLS configuration (private CA, mTLS, server name);
	// setting it enables TLS. Versions below TLS 1.2 are raised to it.
	TLS *tls.Config `json:"-"`
	// SASLMechanism selects the SASL mechanism: "PLAIN" (default),
	// "SCRAM-SHA-256" or "SCRAM-SHA-512". Managed brokers such as OCI Kafka
	// typically require SCRAM. Empty means PLAIN.
	SASLMechanism string
	// AllowPlaintextSASL permits SASL PLAIN without TLS, which sends the
	// password in clear text. Only for local development.
	AllowPlaintextSASL bool
	// Acks is how many replicas must store a record before a send succeeds:
	// "all" (default, with the idempotent producer), "leader" or "none".
	// Anything but "all" disables idempotence and can lose acknowledged
	// records on a leader failover.
	Acks     string
	ClientID string
	// Topics lists topics to be created idempotently when Setup is called.
	Topics []TopicConfig
}

// Errors returned by New for unsafe or inconsistent configurations.
var (
	ErrPartialCredentials = errors.New("kafka: SASL needs both User and Password")
	ErrUnknownMechanism   = errors.New("kafka: unknown SASL mechanism (PLAIN, SCRAM-SHA-256 or SCRAM-SHA-512)")
	ErrPlaintextSASL      = errors.New("kafka: SASL PLAIN without TLS sends the password in clear text; enable TLS or set AllowPlaintextSASL")
)

// clusterAdmin abstracts sarama.ClusterAdmin to allow injection of test doubles.
type clusterAdmin interface {
	CreateTopic(topic string, detail *sarama.TopicDetail, validateOnly bool) error
	Close() error
}

// In-place redelivery bounds for a record the handler nacked.
const (
	redeliverBackoffMin = time.Second
	redeliverBackoffMax = 30 * time.Second
)

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

	// Consumption — sarama's defaults are tuned for minimal latency, not for a
	// topic shared by many consumer groups.
	//
	// Fetch.Min=1 (the default) makes the broker answer as soon as a single byte
	// is available: every member fetches continuously without bringing volume,
	// and the cost is paid in broker CPU rather than throughput. Requiring a
	// minimum per response — capped by MaxWaitTime, so this is batching, not
	// latency starvation — brings the same data in far fewer round trips.
	sc.Consumer.Fetch.Min = 64 * 1024
	sc.Consumer.MaxWaitTime = 500 * time.Millisecond

	// Handlers do network I/O (marketplace API, rate limiter waits). The 100ms
	// default is exceeded by every real message, and each overrun pauses and
	// resumes the partition for nothing.
	sc.Consumer.MaxProcessingTime = 5 * time.Second

	// Sticky keeps the assignment across rebalances; with Range (the default)
	// every pod restart reassigns everything and the whole group stalls. Range
	// stays as a fallback so that a rolling deploy, with members of different
	// versions in the same group, can still negotiate a common strategy instead
	// of failing the join.
	sc.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategySticky(),
		sarama.NewBalanceStrategyRange(),
	}
	if cfg.ClientID != "" {
		sc.ClientID = cfg.ClientID
	}
	if err := applyDurability(sc, cfg.Acks); err != nil {
		return nil, err
	}
	tlsEnabled := cfg.UseTLS || cfg.TLS != nil
	mechanism, err := applySASL(sc, cfg, tlsEnabled)
	if err != nil {
		return nil, err
	}
	// TLS is independent of SASL: managed brokers commonly require SASL_SSL,
	// but plain TLS (no auth) is also valid. Enable via MESSAGING_USE_TLS.
	if tlsEnabled {
		sc.Net.TLS.Enable = true
		sc.Net.TLS.Config = tlsConfig(cfg.TLS)
	}
	if err := sc.Validate(); err != nil {
		return nil, fmt.Errorf("kafka: invalid config: %w", err)
	}

	// Startup diagnostics: make the resolved target explicit so connection
	// failures ("run out of available brokers") are not opaque. Set
	// MESSAGING_DEBUG=true to surface Sarama's per-broker connection errors.
	logging.Info("kafka: broker config",
		slog.Any("brokers", cfg.Brokers),
		slog.Bool("tls", tlsEnabled),
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

// applyDurability makes a confirmed send survive a leader failover by
// default: every in-sync replica stores the record, and the idempotent
// producer keeps retries from duplicating or reordering it.
func applyDurability(sc *sarama.Config, acks string) error {
	switch strings.ToLower(strings.TrimSpace(acks)) {
	case "", "all", "-1":
		sc.Producer.RequiredAcks = sarama.WaitForAll
		sc.Producer.Idempotent = true
		sc.Net.MaxOpenRequests = 1
	case "leader", "1":
		sc.Producer.RequiredAcks = sarama.WaitForLocal
	case "none", "0":
		sc.Producer.RequiredAcks = sarama.NoResponse
	default:
		return fmt.Errorf("kafka: invalid Acks %q (all, leader or none)", acks)
	}
	return nil
}

// applySASL enables SASL when credentials are set and returns the mechanism
// name for logging. Partial credentials, unknown mechanisms and PLAIN over
// plaintext are refused instead of silently degraded.
func applySASL(sc *sarama.Config, cfg Config, tlsEnabled bool) (string, error) {
	mechanism := strings.ToUpper(strings.TrimSpace(cfg.SASLMechanism))
	if cfg.User == "" && cfg.Password == "" {
		return "none", nil
	}
	if cfg.User == "" || cfg.Password == "" {
		return "", ErrPartialCredentials
	}
	sc.Net.SASL.Enable = true
	sc.Net.SASL.User = cfg.User
	sc.Net.SASL.Password = cfg.Password
	switch mechanism {
	case "", "PLAIN":
		mechanism = "PLAIN"
		if !tlsEnabled && !cfg.AllowPlaintextSASL {
			return "", ErrPlaintextSASL
		}
		sc.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	case "SCRAM-SHA-256":
		sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &scramClient{HashGeneratorFcn: sha256GeneratorFcn}
		}
	case "SCRAM-SHA-512":
		sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		sc.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &scramClient{HashGeneratorFcn: sha512GeneratorFcn}
		}
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownMechanism, cfg.SASLMechanism)
	}
	return mechanism, nil
}

// tlsConfig returns a TLS 1.2+ copy of custom, or the default when nil.
func tlsConfig(custom *tls.Config) *tls.Config {
	if custom == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	c := custom.Clone()
	c.MinVersion = max(c.MinVersion, tls.VersionTLS12)
	return c
}

// String implements fmt.Stringer with the password redacted.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with the password redacted.
func (c Config) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with the password redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the configuration with the password redacted.
func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

type plainConfig Config

func (c Config) redacted() plainConfig {
	if c.Password != "" {
		c.Password = "[REDACTED]"
	}
	c.TLS = nil // holds private keys
	return plainConfig(c)
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

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	groupID := cfg.GroupID
	if groupID == "" {
		groupID = cfg.Topic
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
		cfg: cfg,
		newGroup: func() (sarama.ConsumerGroup, error) {
			return sarama.NewConsumerGroup(brokers, groupID, &sc)
		},
	}, nil
}

// Producer

type kafkaProducer struct{ producer sarama.SyncProducer }

func (p *kafkaProducer) SendMessage(_ context.Context, msg *types.Message) error {
	_, _, err := p.producer.SendMessage(encode(msg))
	return err
}

func (p *kafkaProducer) SendMessagesBatch(_ context.Context, msgs []*types.Message) error {
	batch := make([]*sarama.ProducerMessage, 0, len(msgs))
	for _, m := range msgs {
		batch = append(batch, encode(m))
	}
	return p.producer.SendMessages(batch)
}

// encode writes CloudEvents binary mode: the value is the payload and the
// attributes are record headers, so Id, type and time survive the trip.
func encode(m *types.Message) *sarama.ProducerMessage {
	value, headers := types.KafkaBinding.Encode(m)
	pm := &sarama.ProducerMessage{
		Topic:   m.Topic,
		Value:   sarama.ByteEncoder(value),
		Headers: toRecordHeaders(headers),
	}
	if m.Key != "" {
		pm.Key = sarama.StringEncoder(m.Key)
	}
	if !m.Timestamp.IsZero() {
		pm.Timestamp = m.Timestamp
	}
	return pm
}

// decode reads CloudEvents records and, for producers without them, keeps the
// record's key, timestamp and headers with an Id derived from the record's
// position, so a redelivered record keeps its Id.
func decode(sm *sarama.ConsumerMessage) *types.Message {
	headers := fromRecordHeaders(sm.Headers)
	var m types.Message
	if types.KafkaBinding.IsBinary(headers) {
		m = types.KafkaBinding.Decode(sm.Value, headers)
	} else {
		m = types.Message{Value: sm.Value, Headers: headers}
	}
	if m.Id == uuid.Nil {
		m.Id = types.StableID(fmt.Sprintf("kafka/%s/%d/%d", sm.Topic, sm.Partition, sm.Offset))
	}
	m.Topic = sm.Topic
	m.Key = string(sm.Key)
	if m.Timestamp.IsZero() {
		m.Timestamp = sm.Timestamp
	}
	return &m
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
	cfg      types.ConsumeConfig
	newGroup func() (sarama.ConsumerGroup, error)

	mu     sync.Mutex
	group  sarama.ConsumerGroup // set while Consume runs
	paused bool
}

// Consume joins the group as ONE member. Sarama runs ConsumeClaim for every
// assigned partition in its own goroutine, so a pod processes its share of
// partitions in parallel while each partition keeps its order.
func (c *kafkaConsumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	group, err := c.newGroup()
	if err != nil {
		return fmt.Errorf("kafka consumer: create consumer group: %w", err)
	}
	defer group.Close()

	c.mu.Lock()
	c.group = group
	if c.paused {
		group.PauseAll()
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.group = nil
		c.mu.Unlock()
	}()

	topics := []string{c.cfg.Topic}
	h := &groupHandler{handler: handler, cfg: c.cfg}
	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		err := group.Consume(ctx, topics, h)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			backoff.Reset() // session ended by a rebalance
			continue
		}
		logging.Error("kafka consumer: session error",
			slog.String("topic", c.cfg.Topic),
			slog.Any("error", err))
		_ = worker.Sleep(ctx, backoff.Next())
	}
}

// Close is a no-op: Consume closes the group when its context ends.
func (c *kafkaConsumer) Close() error { return nil }

// Pause stops fetching on every assigned partition; the membership is kept,
// so no rebalance happens.
func (c *kafkaConsumer) Pause() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = true
	if c.group != nil {
		c.group.PauseAll()
	}
	return nil
}

func (c *kafkaConsumer) Resume() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = false
	if c.group != nil {
		c.group.ResumeAll()
	}
	return nil
}

// groupHandler implements sarama.ConsumerGroupHandler.
type groupHandler struct {
	handler port.MessageHandler
	cfg     types.ConsumeConfig
}

func (h *groupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *groupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	// Offset obfuscation signal: a NEGATIVE start (newest/oldest sentinel) means
	// Sarama had no usable committed offset (missing OR out-of-range because
	// retention deleted past the commit) and fell back to the policy. It only
	// matters when there IS data in the topic (hwm>0): newest skipping a real
	// backlog is the blind spot that hides "not consuming" behind a stale
	// offset. WARN only in that case — a group that is new on an empty topic
	// (hwm==0) is normal and stays silent.
	if start := claim.InitialOffset(); start == sarama.OffsetNewest && claim.HighWaterMarkOffset() > 0 {
		logging.Warn("kafka consumer: claim started at the END — no valid committed offset, existing backlog SKIPPED",
			slog.String("topic", claim.Topic()),
			slog.String("group_id", h.cfg.GroupID),
			slog.Int("partition", int(claim.Partition())),
			slog.Int64("high_water_mark", claim.HighWaterMarkOffset()))
	}

	for sm := range claim.Messages() {
		if !h.process(session, sm) {
			// Unmarked: the next owner of the partition reprocesses the record.
			return nil
		}
		// Always mark once settled: Sarama's auto-commit only flushes marked
		// offsets, so skipping this means the group never commits at all.
		session.MarkMessage(sm, "")
	}
	return nil
}

// process runs the handler until the record is settled (Ack, Ignore, Reject
// or dead lettered by the pipeline). Each in-place redelivery counts as a
// delivery, so the pipeline's MaxDeliveries ends a poison record. Kafka cannot redeliver one record without
// rewinding the partition, and marking a Nack would commit past it and lose
// it, so a Nack is redelivered in place with backoff: the partition waits,
// which keeps per-key order. It reports false when the session ended first.
func (h *groupHandler) process(session sarama.ConsumerGroupSession, sm *sarama.ConsumerMessage) bool {
	ctx := session.Context()
	backoff := worker.Backoff{Min: cmp.Or(h.cfg.RetryBackoff, redeliverBackoffMin), Max: redeliverBackoffMax}
	backoff.Max = max(backoff.Max, backoff.Min)
	for attempt := 1; ; attempt++ {
		// Retries and dead-lettering run in msq's core pipeline; the session
		// context lets it stop waiting when the partition is revoked.
		m := decode(sm)
		m.DeliveryCount = attempt
		result, err := h.handler.Handle(ctx, m)
		if result != types.Nack {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		logging.Error("kafka consumer: record nacked, redelivering in place (partition blocked)",
			slog.String("topic", sm.Topic),
			slog.Int("partition", int(sm.Partition)),
			slog.Int64("offset", sm.Offset),
			slog.Int("attempt", attempt),
			slog.Any("error", err))
		if worker.Sleep(ctx, backoff.Next()) != nil {
			return false
		}
	}
}

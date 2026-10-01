// Package nats implements port.Broker for NATS JetStream: persistent
// subjects, durable consumer groups, explicit acks and redelivery. Messages
// use CloudEvents binary mode and the message Id as the JetStream
// de-duplication id.
package nats

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/msq/types"
	"github.com/gofi-labs/gofi-sdk-go/msq/worker"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config configures the connection and the streams created on demand.
// Printing or logging it redacts the token and password.
type Config struct {
	// URL is one or more comma-separated server URLs (nats://host:4222).
	URL  string
	Name string // client name shown in server monitoring

	// TLS is the client TLS configuration (private CA, mTLS, server name);
	// setting it requires TLS. Versions below TLS 1.2 are raised to it.
	TLS *tls.Config `json:"-"`

	// Authentication: credentials file (JWT + NKey), token or user/password.
	CredsFile string
	Token     string
	User      string
	Password  string

	// Streams created for subjects without one: Replicas (default 1) and
	// MaxAge, the retention bound (0 = no age limit).
	Replicas int
	MaxAge   time.Duration

	// AckWait is how long an unacked message waits before redelivery when
	// ConsumeConfig.VisibilityTimeout is zero (default 30s). It applies when
	// the durable consumer is created; the lease is renewed while handling.
	AckWait time.Duration
}

// Broker implements port.Broker and port.BrokerSetup.
type Broker struct {
	nc      *nats.Conn
	js      jetstream.JetStream
	cfg     Config
	streams sync.Map // subject -> stream name
}

// New connects; it reconnects forever in the background.
func New(cfg Config) (*Broker, error) {
	opts := []nats.Option{nats.MaxReconnects(-1), nats.Name(cfg.Name)}
	if cfg.TLS != nil {
		t := cfg.TLS.Clone()
		t.MinVersion = max(t.MinVersion, tls.VersionTLS12)
		opts = append(opts, nats.Secure(t))
	}
	switch {
	case cfg.CredsFile != "":
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	case cfg.Token != "":
		opts = append(opts, nats.Token(cfg.Token))
	case cfg.User != "":
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Password))
	}
	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream: %w", err)
	}
	return &Broker{nc: nc, js: js, cfg: cfg}, nil
}

// String implements fmt.Stringer with the secrets redacted.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString implements fmt.GoStringer with the secrets redacted.
func (c Config) GoString() string { return fmt.Sprintf("%#v", c.redacted()) }

// LogValue implements slog.LogValuer with the secrets redacted.
func (c Config) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalJSON encodes the configuration with the secrets redacted.
func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

type plainConfig Config

func (c Config) redacted() plainConfig {
	for _, s := range []*string{&c.Token, &c.Password} {
		if *s != "" {
			*s = "[REDACTED]"
		}
	}
	c.TLS = nil // holds private keys
	return plainConfig(c)
}

// Setup checks that JetStream is enabled on the server.
func (b *Broker) Setup(ctx context.Context) error {
	if _, err := b.js.AccountInfo(ctx); err != nil {
		return fmt.Errorf("nats: jetstream unavailable: %w", err)
	}
	return nil
}

// Close drains the connection: pending publishes and acks are flushed.
func (b *Broker) Close() error { return b.nc.Drain() }

// stream returns the stream capturing subject, creating one named after the
// subject when none exists. Streams defined by operators are never modified.
func (b *Broker) stream(ctx context.Context, subject string) (string, error) {
	if name, ok := b.streams.Load(subject); ok {
		return name.(string), nil
	}
	name, err := b.js.StreamNameBySubject(ctx, subject)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		name = streamName(subject)
		_, err = b.js.CreateStream(ctx, jetstream.StreamConfig{
			Name:     name,
			Subjects: []string{subject},
			Replicas: max(b.cfg.Replicas, 1),
			MaxAge:   b.cfg.MaxAge,
			Storage:  jetstream.FileStorage,
		})
		if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("nats: stream for %q: %w", subject, err)
	}
	b.streams.Store(subject, name)
	return name, nil
}

var nameReplacer = strings.NewReplacer(".", "_", "*", "_", ">", "_", " ", "_")

func streamName(subject string) string { return nameReplacer.Replace(subject) }

func (b *Broker) NewProducer() (port.Producer, error) { return &producer{b: b}, nil }

func (b *Broker) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	if cfg.Topic == "" {
		return nil, errors.New("nats: ConsumeConfig.Topic (subject) is required")
	}
	return &consumer{b: b, cfg: cfg, concurrency: max(cfg.Concurrency, 1)}, nil
}

// Producer

type producer struct{ b *Broker }

func (p *producer) msg(ctx context.Context, m *types.Message) (*nats.Msg, error) {
	if m.Topic == "" {
		return nil, errors.New("nats producer: msg.Topic (subject) must be set")
	}
	if _, err := p.b.stream(ctx, m.Topic); err != nil {
		return nil, err
	}
	body, headers := types.NATSBinding.Encode(m)
	nm := nats.NewMsg(m.Topic)
	nm.Data = body
	for k, v := range headers {
		nm.Header.Set(k, v)
	}
	return nm, nil
}

// SendMessage returns once the stream stored the message; resending the same
// Id within the duplicate window is ignored by the server.
func (p *producer) SendMessage(ctx context.Context, m *types.Message) error {
	nm, err := p.msg(ctx, m)
	if err != nil {
		return err
	}
	if _, err := p.b.js.PublishMsg(ctx, nm, jetstream.WithMsgID(m.Id.String())); err != nil {
		return fmt.Errorf("nats producer: publish %q: %w", m.Topic, err)
	}
	return nil
}

// SendMessagesBatch publishes asynchronously and waits for every ack.
func (p *producer) SendMessagesBatch(ctx context.Context, msgs []*types.Message) error {
	futures := make([]jetstream.PubAckFuture, 0, len(msgs))
	for _, m := range msgs {
		nm, err := p.msg(ctx, m)
		if err != nil {
			return err
		}
		f, err := p.b.js.PublishMsgAsync(nm, jetstream.WithMsgID(m.Id.String()))
		if err != nil {
			return fmt.Errorf("nats producer: publish %q: %w", m.Topic, err)
		}
		futures = append(futures, f)
	}
	var errs []error
	for _, f := range futures {
		select {
		case <-f.Ok():
		case err := <-f.Err():
			errs = append(errs, err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.Join(errs...)
}

func (p *producer) Close() error { return nil }

// Consumer

// fetchWait bounds one pull, so Pause and shutdown are seen promptly.
const fetchWait = 2 * time.Second

type consumer struct {
	b           *Broker
	cfg         types.ConsumeConfig
	concurrency int
	gate        worker.Gate
}

// Consume attaches to the durable consumer (GroupID, default the subject):
// instances sharing it split the messages.
func (c *consumer) Consume(ctx context.Context, handler port.MessageHandler) error {
	stream, err := c.b.stream(ctx, c.cfg.Topic)
	if err != nil {
		return err
	}
	name := streamName(c.cfg.GroupID)
	if name == "" {
		name = streamName(c.cfg.Topic)
	}
	cons, err := c.b.js.Consumer(ctx, stream, name)
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		deliver := jetstream.DeliverNewPolicy
		if c.cfg.InitialOffset == types.OffsetResetEarliest {
			deliver = jetstream.DeliverAllPolicy
		}
		cons, err = c.b.js.CreateConsumer(ctx, stream, jetstream.ConsumerConfig{
			Durable:       name,
			FilterSubject: c.cfg.Topic,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: deliver,
			AckWait:       cmp.Or(c.cfg.VisibilityTimeout, c.b.cfg.AckWait, types.DefaultVisibilityTimeout),
			MaxAckPending: c.concurrency * 4,
			// Server-side backstop of the pipeline's delivery limit: also ends
			// messages whose handler never settles them (crash loops). 0 = -1.
			MaxDeliver: c.cfg.DeliveryLimit(),
		})
	}
	if err != nil {
		return fmt.Errorf("nats consumer: %q: %w", name, err)
	}
	// Renew against the durable's actual AckWait: an existing one keeps its own.
	renewEvery := worker.RenewEvery(cmp.Or(cons.CachedInfo().Config.AckWait, types.DefaultVisibilityTimeout))

	pool := worker.New(c.concurrency)
	defer pool.Close()
	slots := worker.NewSlots(c.concurrency)
	backoff := worker.Backoff{Min: worker.ReceiveBackoffMin, Max: worker.ReceiveBackoffMax}
	for {
		if c.gate.Wait(ctx) != nil || ctx.Err() != nil {
			return nil
		}
		// Pull only what can start now: a prefetched message's AckWait would
		// run out before its handler even starts.
		free, err := slots.Acquire(ctx, c.concurrency)
		if err != nil {
			continue
		}
		if err := c.fetch(ctx, cons, free, renewEvery, pool, slots, handler); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logging.Warn("nats consumer: fetch failed", slog.String("subject", c.cfg.Topic), slog.Any("error", err))
			_ = worker.Sleep(ctx, backoff.Next())
			continue
		}
		backoff.Reset()
	}
}

// fetch pulls up to free messages; each holds one of the acquired slots until
// its handler finishes, and unused slots are released.
func (c *consumer) fetch(ctx context.Context, cons jetstream.Consumer, free int, renewEvery time.Duration,
	pool *worker.Pool, slots *worker.Slots, handler port.MessageHandler) error {
	n := 0
	defer func() { slots.Release(free - n) }()
	fctx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()
	batch, err := cons.Fetch(free, jetstream.FetchContext(fctx))
	if err != nil {
		return err
	}
	for jm := range batch.Messages() {
		n++
		stop := worker.KeepAlive(ctx, renewEvery, func(context.Context) {
			if err := jm.InProgress(); err != nil {
				logging.Warn("nats consumer: ack wait renewal failed", slog.String("subject", jm.Subject()), slog.Any("error", err))
			}
		})
		pool.Enqueue(func() {
			defer slots.Release(1)
			c.handle(ctx, jm, handler, stop)
		})
	}
	if err := batch.Error(); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Bounds of the delay the server waits before redelivering a nacked message.
const (
	nakDelayMin = time.Second
	nakDelayMax = 30 * time.Second
)

// handle stops the lease renewal before settling, so InProgress never races
// the ack.
func (c *consumer) handle(ctx context.Context, jm jetstream.Msg, handler port.MessageHandler, stopLease func()) {
	m := decode(jm)
	result, err := handler.Handle(ctx, &m)
	stopLease()
	var ackErr error
	switch result {
	case types.Nack:
		// Redelivered by the server after a growing delay: no hot loop, and
		// no worker held while waiting.
		delay := worker.RedeliveryDelay(cmp.Or(c.cfg.RetryBackoff, nakDelayMin), nakDelayMax, m.DeliveryCount)
		logging.Warn("nats consumer: handler nacked, redelivering", slog.String("subject", jm.Subject()),
			slog.Int("deliveries", m.DeliveryCount), slog.Duration("delay", delay), slog.Any("error", err))
		ackErr = jm.NakWithDelay(delay)
	case types.Reject:
		// Never redelivered; the server publishes a terminated advisory.
		ackErr = jm.TermWithReason("msq: delivery limit reached")
	default:
		ackErr = jm.Ack()
	}
	if ackErr != nil {
		logging.Error("nats consumer: ack failed", slog.String("subject", jm.Subject()), slog.Any("error", ackErr))
	}
}

// decode sets Topic to the message's subject and DeliveryCount from the
// JetStream metadata; a message without an Id gets one from its stream
// sequence, so it keeps it across redeliveries.
func decode(jm jetstream.Msg) types.Message {
	headers := make(map[string]string, len(jm.Headers()))
	for k, v := range jm.Headers() {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	var m types.Message
	if types.NATSBinding.IsBinary(headers) {
		m = types.NATSBinding.Decode(jm.Data(), headers)
	} else {
		m = types.DecodeEnvelope(jm.Data())
	}
	m.Topic = jm.Subject()
	meta, err := jm.Metadata()
	if err == nil {
		m.DeliveryCount = int(min(meta.NumDelivered, math.MaxInt32)) // #nosec G115 -- capped
	}
	if m.Id == uuid.Nil {
		if err == nil {
			m.Id = types.StableID(fmt.Sprintf("nats/%s/%d", meta.Stream, meta.Sequence.Stream))
		} else {
			m.Id = uuid.New()
		}
	}
	return m
}

func (c *consumer) Close() error  { return nil }
func (c *consumer) Pause() error  { c.gate.Pause(); return nil }
func (c *consumer) Resume() error { c.gate.Resume(); return nil }

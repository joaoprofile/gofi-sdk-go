package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
	"github.com/joaoprofile/gofi-sdk-go/msq/worker"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// DefaultHealthGrace is how long a consumer may be down (restarting) before
// Healthy reports it.
const DefaultHealthGrace = 30 * time.Second

// Restart backoff bounds of a failed consumer.
const (
	restartBackoffMin = time.Second
	restartBackoffMax = 30 * time.Second
)

// ConsumerManager orchestrates multiple consumers against a single BrokerService.
// Register all consumers before calling Start or Dispatcher.
//
// A consumer whose Consume fails (or returns before shutdown) is recreated
// and restarted with exponential backoff until the manager closes; Healthy
// reports consumers that stay down longer than the health grace.
type ConsumerManager struct {
	broker *BrokerService
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	done   chan struct{} // closed once every consumer has stopped

	mu      sync.Mutex // guards the fields below
	entries []entry
	started int // entries[:started] are already running
	states  []*consumerState
	closed  bool
	grace   time.Duration
}

type entry struct {
	cfg     types.ConsumeConfig
	handler port.MessageHandler
}

// NewConsumerManager creates a ConsumerManager backed by the given Broker.
// A plain provider broker is wrapped in a BrokerService so every consumer
// runs through the pipeline.
func NewConsumerManager(broker port.Broker) *ConsumerManager {
	svc, ok := broker.(*BrokerService)
	if !ok {
		svc = NewService(ServiceConfig{Broker: broker})
	}
	return newConsumerManager(svc)
}

func newConsumerManager(svc *BrokerService) *ConsumerManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &ConsumerManager{broker: svc, ctx: ctx, cancel: cancel, done: make(chan struct{}), grace: DefaultHealthGrace}
	svc.track(m)
	return m
}

// SetHealthGrace sets how long a consumer may be down before Healthy reports
// it (default DefaultHealthGrace).
func (m *ConsumerManager) SetHealthGrace(d time.Duration) *ConsumerManager {
	m.mu.Lock()
	m.grace = max(d, 0)
	m.mu.Unlock()
	return m
}

// Register adds a consumer for the given topic configuration.
// The handler func is automatically adapted to the MessageHandler interface.
func (m *ConsumerManager) Register(cfg types.ConsumeConfig, handler func(ctx context.Context, msg *types.Message) (types.Result, error)) *ConsumerManager {
	return m.RegisterHandler(cfg, port.MessageHandlerFunc(handler))
}

// RegisterHandler adds a consumer using the full MessageHandler interface.
// Use when your handler is a struct that implements port.MessageHandler.
func (m *ConsumerManager) RegisterHandler(cfg types.ConsumeConfig, handler port.MessageHandler) *ConsumerManager {
	m.mu.Lock()
	m.entries = append(m.entries, entry{cfg: cfg, handler: handler})
	m.mu.Unlock()
	return m
}

// Dispatcher sets the default concurrency for entries without one and starts
// the consumers registered since the last start. It returns the joined errors
// of consumers that could not be created; those are retried in the
// background and the others keep running.
func (m *ConsumerManager) Dispatcher(concurrency int) error {
	m.mu.Lock()
	for i := m.started; i < len(m.entries); i++ {
		if m.entries[i].cfg.Concurrency <= 0 {
			m.entries[i].cfg.Concurrency = concurrency
		}
	}
	m.mu.Unlock()
	return m.start()
}

// Start launches the consumers registered since the last start, using the
// concurrency in each ConsumeConfig. It returns the joined creation errors;
// failed consumers are retried in the background. After Close it returns
// ErrManagerClosed.
func (m *ConsumerManager) Start() error {
	return m.start()
}

func (m *ConsumerManager) start() error {
	// Only entries registered since the last start: calling Start twice must not
	// duplicate consumers.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	pending := append([]entry(nil), m.entries[m.started:]...)
	m.started = len(m.entries)
	m.mu.Unlock()

	var errs []error
	for _, e := range pending {
		if e.cfg.Topic == "" {
			errs = append(errs, ErrTopicRequired)
			continue
		}

		consumer, err := m.newConsumer(e.cfg)
		st := &consumerState{topic: e.cfg.Topic}
		if err != nil {
			errs = append(errs, err)
			st.down(err, false)
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			if consumer != nil {
				m.closeConsumer(consumer, e.cfg.Topic)
			}
			errs = append(errs, ErrManagerClosed)
			continue
		}
		m.states = append(m.states, st)
		m.wg.Add(1)
		m.mu.Unlock()
		go m.supervise(consumer, e, st)
	}
	return errors.Join(errs...)
}

func (m *ConsumerManager) newConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	c, err := m.broker.NewConsumer(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: topic %q: %w", ErrConsumerFailed, cfg.Topic, err)
	}
	if c == nil {
		return nil, fmt.Errorf("%w: topic %q: nil consumer", ErrConsumerFailed, cfg.Topic)
	}
	return c, nil
}

// supervise runs the consumer until the manager closes, recreating it with
// backoff whenever it fails or stops on its own. c is nil when the first
// creation failed.
func (m *ConsumerManager) supervise(c port.Consumer, e entry, st *consumerState) {
	defer m.wg.Done()
	topic := e.cfg.Topic
	backoff := worker.Backoff{Min: restartBackoffMin, Max: restartBackoffMax}
	for {
		for c == nil {
			d := backoff.Next()
			if worker.Sleep(m.ctx, d) != nil {
				st.stop()
				return
			}
			var err error
			if c, err = m.newConsumer(e.cfg); err != nil {
				st.down(err, false)
				logging.Error("ConsumerManager: consumer creation failed, retrying",
					slog.String("topic", topic), slog.Any("error", err))
			}
		}

		st.up()
		began := time.Now()
		logging.Info("ConsumerManager: consumer started", slog.String("topic", topic))
		err := c.Consume(m.ctx, e.handler)
		m.closeConsumer(c, topic)
		c = nil
		if m.ctx.Err() != nil {
			st.stop()
			logging.Info("ConsumerManager: consumer stopped", slog.String("topic", topic))
			return
		}
		if err == nil {
			err = errConsumerExited
		}
		ranLong := time.Since(began) >= m.healthGrace()
		if ranLong {
			backoff.Reset()
		}
		st.down(err, ranLong)
		logging.Error("ConsumerManager: consumer failed, restarting",
			slog.String("topic", topic), slog.Any("error", err))
		m.broker.emit(m.ctx, types.BrokerEvent{Type: types.EventConsumerRestarting, Topic: topic, Error: err, Timestamp: time.Now()})
	}
}

var errConsumerExited = errors.New("msq: consumer stopped before shutdown")

func (m *ConsumerManager) closeConsumer(c port.Consumer, topic string) {
	if err := c.Close(); err != nil {
		logging.Error("ConsumerManager: error closing consumer",
			slog.String("topic", topic),
			slog.Any("error", err))
	}
}

func (m *ConsumerManager) healthGrace() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.grace
}

// Healthy returns nil while every started consumer is running. A consumer
// down (failed and restarting) for longer than the health grace, or a
// closed manager, is reported. Wire it into the readiness check.
func (m *ConsumerManager) Healthy() error {
	m.mu.Lock()
	closed, grace := m.closed, m.grace
	states := append([]*consumerState(nil), m.states...)
	m.mu.Unlock()
	if closed {
		return ErrManagerClosed
	}
	now := time.Now()
	var errs []error
	for _, st := range states {
		if err := st.health(now, grace); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close signals all consumers to stop and waits until in-flight handlers
// return. It is idempotent: a second caller (e.g. BrokerService.Close) blocks
// until the drain finishes, which keeps DB/cache pools alive meanwhile.
// Handlers are bounded by ConsumeConfig.HandlerTimeout; use CloseContext to
// bound the wait itself.
func (m *ConsumerManager) Close() {
	_ = m.CloseContext(context.Background())
}

// CloseContext is Close bounded by ctx: when ctx ends before the drain
// finishes it returns an error wrapping ctx.Err() while consumers keep
// draining in the background. Calling it again waits for the same drain.
func (m *ConsumerManager) CloseContext(ctx context.Context) error {
	m.mu.Lock()
	first := !m.closed
	m.closed = true
	m.mu.Unlock()
	if first {
		logging.Info("ConsumerManager: initiating graceful shutdown...")
		m.cancel()
		go func() {
			m.wg.Wait()
			logging.Info("ConsumerManager: all consumers stopped")
			close(m.done)
		}()
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("msq: consumers still draining: %w", ctx.Err())
	}
}

// Shutdown is an alias for Close.
func (m *ConsumerManager) Shutdown() { m.Close() }

// consumerState tracks one supervised consumer for Healthy.
type consumerState struct {
	topic string

	mu        sync.Mutex
	running   bool
	downSince time.Time // start of the current outage; zero when not down
	err       error
}

func (s *consumerState) up() {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()
}

// down records a failure. A failure after a long run starts a new outage;
// quick failures extend the current one so a crash loop stays reported.
func (s *consumerState) down(err error, newOutage bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	s.err = err
	if newOutage || s.downSince.IsZero() {
		s.downSince = time.Now()
	}
}

func (s *consumerState) stop() {
	s.mu.Lock()
	s.running = false
	s.downSince = time.Time{}
	s.mu.Unlock()
}

func (s *consumerState) health(now time.Time, grace time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running || s.downSince.IsZero() {
		return nil
	}
	if down := now.Sub(s.downSince); down >= grace {
		return fmt.Errorf("%w: topic %q down for %s: %w", ErrConsumerDown, s.topic, down.Round(time.Second), s.err)
	}
	return nil
}

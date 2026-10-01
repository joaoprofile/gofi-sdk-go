// Package core implements the orchestration layer of the msq messaging system.
// It depends only on port interfaces and types — never on provider implementations.
package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/msq/port"
	"github.com/joaoprofile/gofi-sdk-go/msq/types"
)

// BrokerService is the central facade of the msq package, obtained via msq.New.
//
// Producers and consumers it creates share one pipeline: trace propagation,
// OpenTelemetry spans and metrics, OnEvent events, handler panic recovery,
// retries, dead-lettering and a shutdown that lets in-flight handlers finish.
type BrokerService struct {
	broker   port.Broker
	tel      telemetry
	emit     func(ctx context.Context, event types.BrokerEvent)
	defaults ConsumeDefaults

	mu       sync.Mutex
	managers []*ConsumerManager
}

// ServiceConfig holds the internal parameters for building a BrokerService.
type ServiceConfig struct {
	Broker port.Broker
	// System is the messaging.system attribute (kafka, rabbitmq, aws_sqs, ...).
	System  string
	OnEvent func(ctx context.Context, event types.BrokerEvent)
	// Defaults fill ConsumeConfig fields left at zero.
	Defaults ConsumeDefaults
}

// ConsumeDefaults are service-wide values for ConsumeConfig fields a consumer
// leaves at zero (e.g. from MESSAGING_* variables).
type ConsumeDefaults struct {
	MaxDeliveries  int
	HandlerTimeout time.Duration
}

func (d ConsumeDefaults) apply(cfg types.ConsumeConfig) types.ConsumeConfig {
	if cfg.MaxDeliveries == 0 {
		cfg.MaxDeliveries = d.MaxDeliveries
	}
	if cfg.HandlerTimeout == 0 {
		cfg.HandlerTimeout = d.HandlerTimeout
	}
	return cfg
}

// NewService builds a BrokerService from validated configuration.
// Use msq.New instead of calling this directly.
func NewService(cfg ServiceConfig) *BrokerService {
	emit := cfg.OnEvent
	if emit == nil {
		emit = func(context.Context, types.BrokerEvent) {}
	}
	return &BrokerService{broker: cfg.Broker, tel: telemetry{system: cfg.System}, emit: emit, defaults: cfg.Defaults}
}

// NewProducer returns an instrumented Producer from the underlying broker.
func (s *BrokerService) NewProducer() (port.Producer, error) {
	p, err := s.broker.NewProducer()
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("msq: broker returned a nil producer")
	}
	return &producer{Producer: p, tel: s.tel, emit: s.emit}, nil
}

// NewConsumer returns a Consumer whose handler runs through the pipeline.
// With DeadLetterTopic set it also owns a producer for dead letters.
func (s *BrokerService) NewConsumer(cfg types.ConsumeConfig) (port.Consumer, error) {
	cfg = s.defaults.apply(cfg)
	c, err := s.broker.NewConsumer(cfg)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("msq: broker returned a nil consumer")
	}
	wrapped := &consumer{Consumer: c, cfg: cfg, tel: s.tel, emit: s.emit}
	if cfg.DeadLetterTopic != "" {
		dlq, err := s.NewProducer()
		if err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("msq: dead-letter producer: %w", err)
		}
		wrapped.dlq = dlq
	}
	return wrapped, nil
}

// NewConsumerManager returns a ConsumerManager backed by this service; Close
// drains it.
func (s *BrokerService) NewConsumerManager() *ConsumerManager {
	return newConsumerManager(s)
}

func (s *BrokerService) track(m *ConsumerManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managers = append(s.managers, m)
}

// Close drains every ConsumerManager created from this service, waiting for
// in-flight handlers. It does not close the underlying broker. Prefer
// CloseContext to bound the wait.
func (s *BrokerService) Close() error {
	return s.CloseContext(context.Background())
}

// CloseContext drains every ConsumerManager created from this service; gofi's
// builder calls it with the shutdown context before closing the broker and
// the database/cache pools. When ctx ends first it returns an error wrapping
// ctx.Err(). It does not close the underlying broker.
func (s *BrokerService) CloseContext(ctx context.Context) error {
	s.mu.Lock()
	managers := append([]*ConsumerManager(nil), s.managers...)
	s.mu.Unlock()
	errs := make([]error, len(managers))
	var wg sync.WaitGroup
	for i, m := range managers {
		wg.Go(func() { errs[i] = m.CloseContext(ctx) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Healthy returns nil while every consumer of every ConsumerManager created
// from this service is running (see ConsumerManager.Healthy). Wire it into
// the readiness check.
func (s *BrokerService) Healthy() error {
	s.mu.Lock()
	managers := append([]*ConsumerManager(nil), s.managers...)
	s.mu.Unlock()
	var errs []error
	for _, m := range managers {
		if err := m.Healthy(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

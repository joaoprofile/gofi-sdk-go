// Package core implements the orchestration layer of the msq messaging system.
// It depends only on port interfaces and types — never on provider implementations.
package core

import (
	"context"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
)

// BrokerService is the central facade of the msq package.
// Obtained via msq.New() or msq.NewDefault(); callers should not construct it directly.
//
// It wraps any port.Broker provider and adds cross-cutting concerns:
// observability events, middleware hooks, and consumer lifecycle management.
type BrokerService struct {
	broker  port.Broker
	onEvent func(ctx context.Context, event types.BrokerEvent)
}

// ServiceConfig holds the internal parameters for building a BrokerService.
type ServiceConfig struct {
	Broker  port.Broker
	OnEvent func(ctx context.Context, event types.BrokerEvent)
}

// NewService builds a BrokerService from validated configuration.
// Use msq.New() or msq.NewDefault() instead of calling this directly.
func NewService(cfg ServiceConfig) *BrokerService {
	emit := cfg.OnEvent
	if emit == nil {
		emit = func(context.Context, types.BrokerEvent) {}
	}
	return &BrokerService{broker: cfg.Broker, onEvent: emit}
}

// NewProducer returns a ready-to-use Producer from the underlying broker.
func (s *BrokerService) NewProducer() (port.Producer, error) {
	return s.broker.NewProducer()
}

// NewConsumer returns a Consumer configured for the given topic.
func (s *BrokerService) NewConsumer(cfg types.ConsumeConfig) port.Consumer {
	return s.broker.NewConsumer(cfg)
}

// NewConsumerManager returns a ConsumerManager backed by this service.
func (s *BrokerService) NewConsumerManager() *ConsumerManager {
	return NewConsumerManager(s)
}

package core

import (
	"context"
	"log/slog"
	"sync"

	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/types"
	"github.com/joaoprofile/gofi/obs/logging"
)

// ConsumerManager orchestrates multiple consumers against a single BrokerService.
// Register all consumers before calling Start or Dispatcher.
type ConsumerManager struct {
	broker  port.Broker
	entries []entry
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

type entry struct {
	cfg     types.ConsumeConfig
	handler port.MessageHandler
}

// NewConsumerManager creates a ConsumerManager backed by the given Broker.
func NewConsumerManager(broker port.Broker) *ConsumerManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ConsumerManager{broker: broker, ctx: ctx, cancel: cancel}
}

// Register adds a consumer for the given topic configuration.
// The handler func is automatically adapted to the MessageHandler interface.
func (m *ConsumerManager) Register(cfg types.ConsumeConfig, handler func(ctx context.Context, msg *types.Message) (types.Result, error)) *ConsumerManager {
	m.entries = append(m.entries, entry{cfg: cfg, handler: port.MessageHandlerFunc(handler)})
	return m
}

// RegisterHandler adds a consumer using the full MessageHandler interface.
// Use when your handler is a struct that implements port.MessageHandler.
func (m *ConsumerManager) RegisterHandler(cfg types.ConsumeConfig, handler port.MessageHandler) *ConsumerManager {
	m.entries = append(m.entries, entry{cfg: cfg, handler: handler})
	return m
}

// Dispatcher sets per-consumer concurrency and starts all registered consumers.
// Returns self for fluent chaining.
func (m *ConsumerManager) Dispatcher(concurrency int) *ConsumerManager {
	for i := range m.entries {
		if m.entries[i].cfg.Concurrency <= 0 {
			m.entries[i].cfg.Concurrency = concurrency
		}
	}
	m.start()
	return m
}

// Start launches all registered consumers using the concurrency in each ConsumeConfig.
func (m *ConsumerManager) Start() {
	m.start()
}

func (m *ConsumerManager) start() {
	for _, e := range m.entries {
		e := e

		if e.cfg.Topic == "" {
			logging.Error("ConsumerManager: skipping entry with empty topic")
			continue
		}

		consumer := m.broker.NewConsumer(e.cfg)
		if consumer == nil {
			logging.Error("ConsumerManager: broker returned nil consumer",
				slog.String("topic", e.cfg.Topic))
			continue
		}

		m.wg.Add(1)
		go func(c port.Consumer, h port.MessageHandler, topic string) {
			defer m.wg.Done()
			defer func() {
				if err := c.Close(); err != nil {
					logging.Error("ConsumerManager: error closing consumer",
						slog.String("topic", topic),
						slog.Any("error", err))
				}
			}()

			logging.Info("ConsumerManager: consumer started", slog.String("topic", topic))
			if err := c.Consume(m.ctx, h); err != nil {
				logging.Error("ConsumerManager: consumer exited with error",
					slog.String("topic", topic),
					slog.Any("error", err))
			}
			logging.Info("ConsumerManager: consumer stopped", slog.String("topic", topic))
		}(consumer, e.handler, e.cfg.Topic)
	}
}

// Close signals all consumers to stop and waits for graceful shutdown.
func (m *ConsumerManager) Close() {
	logging.Info("ConsumerManager: initiating graceful shutdown...")
	m.cancel()
	m.wg.Wait()
	logging.Info("ConsumerManager: all consumers stopped")
}

// Shutdown is an alias for Close.
func (m *ConsumerManager) Shutdown() { m.Close() }

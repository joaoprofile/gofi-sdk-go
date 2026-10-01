// Package messaging is the gofi component for the message broker (msq).
package messaging

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/msq"
	"github.com/gofi-labs/gofi-sdk-go/msq/port"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

// Component opens the broker and wraps it in the msq pipeline.
type Component struct {
	cfg      msq.Config
	broker   msq.Broker
	injected bool
}

// New registers a messaging provider (RabbitMQ, Kafka, SQS, OCI, Redis, NATS).
//
// Three calling patterns, in order of ergonomics:
//
//	// 1 — zero-config: reads MESSAGING_PROVIDER from env
//	messaging.New()
//
//	// 2 — explicit type, credentials from env
//	messaging.New(msq.Config{BrokerType: msq.BrokerKafka})
//
//	// 3 — explicit broker instance (full control / custom config)
//	messaging.New(msq.Config{Broker: myBroker})
//
// Patterns 1 and 2 need the provider package imported, which keeps unused
// broker SDKs out of the binary:
//
//	import _ "github.com/gofi-labs/gofi-sdk-go/msq/provider/kafka"
//
// If the broker implements port.BrokerSetup, Setup is called during Build so
// that exchanges, topics or queues are declared before the first producer or
// consumer.
func New(cfg ...msq.Config) *Component {
	var c msq.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &Component{cfg: c}
}

// FromBroker uses a broker built and owned by the caller as is: it is not
// wrapped, set up or closed by gofi.
func FromBroker(b msq.Broker) *Component {
	return &Component{broker: b, injected: true}
}

func (c *Component) Name() string      { return "messaging" }
func (c *Component) Stage() gofi.Stage { return gofi.StageMessaging }

func (c *Component) Start(ctx context.Context, rt *gofi.Runtime) error {
	if c.injected {
		return nil
	}
	env := rt.Env()
	cfg := ServiceDefaultsFromEnv(c.cfg, env)

	// Zero-config: derive BrokerType from MESSAGING_PROVIDER.
	if cfg.Broker == nil && cfg.BrokerType == "" {
		provider := env.GetMessagingProvider()
		if provider == "" {
			return errors.New("no broker configured — set BrokerType, pass a Broker, or set MESSAGING_PROVIDER")
		}
		cfg.BrokerType = msq.BrokerType(provider)
	}
	if cfg.OnEvent == nil {
		cfg.OnEvent = logEvent
	}
	if cfg.Broker == nil {
		b, err := msq.Open(ctx, c.providerConfig(env))
		if err != nil {
			return err
		}
		cfg.Broker = b
	}

	svc, err := msq.New(cfg)
	if err != nil {
		return err
	}
	if closer, ok := cfg.Broker.(io.Closer); ok {
		rt.OnClose(func(context.Context) error { return closer.Close() })
	}
	// Registered after the broker, so it runs first: consumers drain, bounded
	// by the shutdown context, before the broker, cache and database close.
	rt.OnClose(svc.CloseContext)
	if setup, ok := cfg.Broker.(port.BrokerSetup); ok {
		if err := setup.Setup(ctx); err != nil {
			return fmt.Errorf("broker setup: %w", err)
		}
	}
	// Not ready while a consumer is down or restarting.
	rt.AddHealthCheck("messaging", func(context.Context) error { return svc.Healthy() })
	c.broker = svc
	return nil
}

// providerConfig is the env ProviderConfig for the declared broker type
// (MESSAGING_PROVIDER when none).
func (c *Component) providerConfig(env *environment.Environment) msq.ProviderConfig {
	pc := ConfigFromEnv(env)
	pc.Type = cmp.Or(c.cfg.BrokerType, pc.Type)
	pc.Exchange = c.cfg.Exchange
	return pc
}

// InsecureTransports reports a broker opened from the environment in
// plaintext or without certificate verification (msq.ProviderConfig.Insecure).
// Brokers passed in, or injected, are the caller's and are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.injected || c.cfg.Broker != nil {
		return nil
	}
	pc := c.providerConfig(env)
	if pc.Type == "" || !pc.Insecure() {
		return nil
	}
	setting, addr := "MESSAGING_USE_TLS / MESSAGING_TLS_*", pc.Addr
	switch {
	case pc.TLS.InsecureSkipVerify:
		setting = "MESSAGING_TLS_INSECURE_SKIP_VERIFY"
	case pc.Type == msq.BrokerRedis:
		setting, addr = "CACHE_USE_TLS / MESSAGING_TLS_*", "at CACHE_URI" // the URI may hold credentials
	}
	return []gofi.InsecureTransport{{
		Resource: core.ResourceMessaging,
		Setting:  setting,
		Detail:   fmt.Sprintf("%s broker %s without verified TLS", pc.Type, addr),
	}}
}

// Broker returns the broker to create producers and consumers; nil before Build.
func (c *Component) Broker() msq.Broker { return c.broker }

// logEvent is the default msq.Config.OnEvent: broker events go to the global logger.
func logEvent(ctx context.Context, ev msq.BrokerEvent) {
	attrs := []any{
		slog.String("event", string(ev.Type)),
		slog.String("topic", ev.Topic),
	}
	if ev.MessageID != "" {
		attrs = append(attrs, slog.String("message_id", ev.MessageID))
	}
	if ev.Error != nil {
		attrs = append(attrs, slog.Any("error", ev.Error))
	}
	logging.Instance().Log(ctx, eventLevel(ev.Type), "messaging event", attrs...)
}

// eventLevel is the log level of a broker event.
func eventLevel(t msq.BrokerEventType) slog.Level {
	switch t {
	case msq.EventProducerError, msq.EventConsumerError,
		msq.EventMessageRejected, msq.EventConsumerRestarting:
		// Rejected messages may be lost; a restarting consumer is not consuming.
		return slog.LevelError
	case msq.EventMessageNacked, msq.EventMessageDeadLettered:
		return slog.LevelWarn
	case msq.EventConsumerStarted, msq.EventConsumerStopped:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

package kafka

import (
	"context"
	"fmt"

	"github.com/gofi-labs/gofi-sdk-go/msq"
)

func init() {
	msq.Register(msq.BrokerKafka, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		c, err := configFrom(cfg)
		if err != nil {
			return nil, err
		}
		b, err := New(c)
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}

func configFrom(cfg msq.ProviderConfig) (Config, error) {
	c := Config{
		Brokers:            []string{cfg.Addr},
		User:               cfg.User,
		Password:           cfg.Password,
		UseTLS:             cfg.TLSEnabled(),
		SASLMechanism:      cfg.SASLMechanism,
		AllowPlaintextSASL: cfg.AllowPlaintextSASL,
	}
	if c.UseTLS {
		t, err := cfg.TLS.Config()
		if err != nil {
			return Config{}, fmt.Errorf("kafka: %w", err)
		}
		c.TLS = t
	}
	return c, nil
}

package nats

import (
	"context"
	"fmt"

	"github.com/gofi-labs/gofi-sdk-go/msq"
)

func init() {
	msq.Register(msq.BrokerNATS, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
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

// configFrom selects tls:// and the TLS block when TLS is enabled.
func configFrom(cfg msq.ProviderConfig) (Config, error) {
	c := Config{
		URL:      "nats://" + cfg.Addr,
		Name:     cfg.ClientName,
		User:     cfg.User,
		Password: cfg.Password,
	}
	if cfg.TLSEnabled() {
		t, err := cfg.TLS.Config()
		if err != nil {
			return Config{}, fmt.Errorf("nats: %w", err)
		}
		c.URL, c.TLS = "tls://"+cfg.Addr, t
	}
	return c, nil
}

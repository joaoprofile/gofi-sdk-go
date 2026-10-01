package redis

import (
	"context"
	"fmt"

	"github.com/joaoprofile/gofi-sdk-go/msq"
)

func init() {
	msq.Register(msq.BrokerRedis, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		c, err := configFrom(cfg)
		if err != nil {
			return nil, err
		}
		switch c.Mode {
		case "", ModePubSub, ModeStreams:
		default:
			return nil, fmt.Errorf("redis: invalid mode %q (pubsub or streams)", c.Mode)
		}
		return New(c), nil
	})
}

// configFrom applies ProviderConfig.TLS to the Redis connection too.
func configFrom(cfg msq.ProviderConfig) (Config, error) {
	c := Config{
		Mode:       Mode(cfg.Redis.Mode),
		Addr:       cfg.Redis.Addr,
		Password:   cfg.Redis.Password,
		TLSEnabled: cfg.RedisTLSEnabled(),
	}
	if c.TLSEnabled {
		t, err := cfg.TLS.Config()
		if err != nil {
			return Config{}, fmt.Errorf("redis: %w", err)
		}
		c.TLS = t
	}
	return c, nil
}

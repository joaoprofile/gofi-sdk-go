package oci

import (
	"context"

	cloudoci "github.com/joaoprofile/gofi-sdk-go/base/cloud/oci"
	"github.com/joaoprofile/gofi-sdk-go/msq"
)

func init() {
	msq.Register(msq.BrokerOCI, func(_ context.Context, cfg msq.ProviderConfig) (msq.Broker, error) {
		b, err := New(configFrom(cfg))
		if err != nil {
			return nil, err
		}
		return b, nil
	})
}

func configFrom(cfg msq.ProviderConfig) Config {
	c := cfg.OCI
	return Config{
		Credentials: cloudoci.Config{
			AuthMode:    cloudoci.AuthMode(c.AuthMode),
			Region:      c.Region,
			TenancyID:   c.TenancyID,
			UserID:      c.UserID,
			Fingerprint: c.Fingerprint,
			PrivateKey:  c.PrivateKey,
		},
	}
}

package cloud

import "github.com/joaoprofile/gofi/base/environment"

func init() {
	RegisterProvider(environment.CLOUD_GCP, func(cfg environment.CloudConfig) Provider {
		return NewGCP(cfg)
	})
}

// GCP implements Provider for Google Cloud Platform.
// Bootstrap and session management are stubs pending a real GCP SDK integration.
type GCP struct {
	cfg environment.CloudConfig
}

func NewGCP(cfg environment.CloudConfig) *GCP {
	return &GCP{cfg: cfg}
}

func (g *GCP) Bootstrap() error {
	return nil
}

// GetSession returns nil until a real GCP client is wired up.
func (g *GCP) GetSession() any {
	return nil
}

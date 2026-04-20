package cloud

import "github.com/joaoprofile/gofi/base/environment"

func init() {
	RegisterProvider(environment.CLOUD_OCI, func(cfg environment.CloudConfig) Provider {
		return NewOCI(cfg)
	})
}

// OCI implements Provider for Oracle Cloud Infrastructure.
// Bootstrap and session management are stubs pending a real OCI SDK integration.
type OCI struct {
	cfg environment.CloudConfig
}

func NewOCI(cfg environment.CloudConfig) *OCI {
	return &OCI{cfg: cfg}
}

func (o *OCI) Bootstrap() error {
	return nil
}

// GetSession returns nil until a real OCI client is wired up.
func (o *OCI) GetSession() any {
	return nil
}

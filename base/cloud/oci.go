package cloud

func init() {
	RegisterProvider(ProviderOCI, func(cfg Config) Provider {
		return NewOCI(cfg)
	})
}

// OCI implements Provider for Oracle Cloud Infrastructure.
// Bootstrap and session management are stubs pending a real OCI SDK integration.
type OCI struct {
	cfg Config
}

func NewOCI(cfg Config) *OCI {
	return &OCI{cfg: cfg}
}

func (o *OCI) Bootstrap() error {
	return nil
}

// GetSession returns nil until a real OCI client is wired up.
func (o *OCI) GetSession() any {
	return nil
}

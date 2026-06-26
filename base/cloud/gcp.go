package cloud

func init() {
	RegisterProvider(ProviderGCP, func(cfg Config) Provider {
		return NewGCP(cfg)
	})
}

// GCP implements Provider for Google Cloud Platform.
// Bootstrap and session management are stubs pending a real GCP SDK integration.
type GCP struct {
	cfg Config
}

func NewGCP(cfg Config) *GCP {
	return &GCP{cfg: cfg}
}

func (g *GCP) Bootstrap() error {
	return nil
}

// GetSession returns nil until a real GCP client is wired up.
func (g *GCP) GetSession() any {
	return nil
}

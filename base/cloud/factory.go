package cloud

import (
	"fmt"
	"sync"

	"github.com/joaoprofile/gofi/base/environment"
)

var errNoProvider = fmt.Errorf("cloud: no provider configured")

// ProviderFactory is a constructor function that builds a Provider from a
// CloudConfig. Factories must not perform I/O — network calls and credential
// validation belong exclusively in Provider.Bootstrap.
type ProviderFactory func(cfg environment.CloudConfig) Provider

var (
	registryMu sync.RWMutex
	registry   = make(map[environment.CloudProvider]ProviderFactory)
)

// RegisterProvider associates a ProviderFactory with a CloudProvider name.
// It is intended to be called from each provider's init() function and panics
// on duplicate registration so wiring errors are caught at startup.
// Safe to call concurrently.
func RegisterProvider(name environment.CloudProvider, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("cloud: provider %q already registered", name))
	}
	registry[name] = factory
}

// newProvider resolves cfg.Provider against the registry and returns the
// matching Provider. It is a pure function — no side-effects, easy to test.
func newProvider(cfg environment.CloudConfig) (Provider, error) {
	if cfg.Provider == environment.CLOUD_NONE || cfg.Provider == "" {
		return nil, errNoProvider
	}
	registryMu.RLock()
	factory, ok := registry[cfg.Provider]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cloud: unsupported provider %q", cfg.Provider)
	}
	return factory(cfg), nil
}

// resetRegistryForTesting clears the registry and re-registers the built-in
// providers. Must only be called from tests.
func resetRegistryForTesting() {
	registryMu.Lock()
	registry = make(map[environment.CloudProvider]ProviderFactory)
	registryMu.Unlock()
	RegisterProvider(environment.CLOUD_AWS, func(cfg environment.CloudConfig) Provider { return NewAWS(cfg) })
	RegisterProvider(environment.CLOUD_GCP, func(cfg environment.CloudConfig) Provider { return NewGCP(cfg) })
	RegisterProvider(environment.CLOUD_OCI, func(cfg environment.CloudConfig) Provider { return NewOCI(cfg) })
}

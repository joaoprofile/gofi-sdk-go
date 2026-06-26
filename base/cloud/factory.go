package cloud

import (
	"fmt"
	"sync"
)

var errNoProvider = fmt.Errorf("cloud: no provider configured")

// ProviderFactory is a constructor function that builds a Provider from a
// Config. Factories must not perform I/O — network calls and credential
// validation belong exclusively in Provider.Bootstrap.
type ProviderFactory func(cfg Config) Provider

var (
	registryMu sync.RWMutex
	registry   = make(map[ProviderName]ProviderFactory)
)

// RegisterProvider associates a ProviderFactory with a provider name.
// It is intended to be called from each provider's init() function and panics
// on duplicate registration so wiring errors are caught at startup.
// Safe to call concurrently.
func RegisterProvider(name ProviderName, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("cloud: provider %q already registered", name))
	}
	registry[name] = factory
}

// newProvider resolves cfg.Provider against the registry and returns the
// matching Provider. It is a pure function — no side-effects, easy to test.
func newProvider(cfg Config) (Provider, error) {
	if cfg.Provider == ProviderNone || cfg.Provider == "" {
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
	registry = make(map[ProviderName]ProviderFactory)
	registryMu.Unlock()
	RegisterProvider(ProviderAWS, func(cfg Config) Provider { return NewAWS(cfg) })
	RegisterProvider(ProviderGCP, func(cfg Config) Provider { return NewGCP(cfg) })
	RegisterProvider(ProviderOCI, func(cfg Config) Provider { return NewOCI(cfg) })
}

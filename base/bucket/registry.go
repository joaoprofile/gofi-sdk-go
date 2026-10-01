package bucket

import (
	"context"
	"fmt"
	"sync"
)

// Opener builds a Store for a provider from a generic Config.
type Opener func(ctx context.Context, cfg Config) (Store, error)

var (
	openersMu sync.RWMutex
	openers   = map[Provider]Opener{}
)

// Register makes a provider available to Open. Provider packages call it from
// init, so importing one (e.g. _ ".../base/bucket/oci") is enough to enable it.
func Register(p Provider, o Opener) {
	openersMu.Lock()
	defer openersMu.Unlock()
	openers[p] = o
}

// Open builds the Store for cfg.Provider. It fails when the provider is not
// set or its package was not imported.
func Open(ctx context.Context, cfg Config) (Store, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("%w: provider is not set", ErrInvalidConfig)
	}
	openersMu.RLock()
	o, ok := openers[cfg.Provider]
	openersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: provider %q is not registered; import _ \"github.com/gofi-labs/gofi-sdk-go/base/bucket/%s\"",
			ErrInvalidConfig, cfg.Provider, packageFor(cfg.Provider))
	}
	return o(ctx, cfg)
}

func packageFor(p Provider) string {
	if p == ProviderMinIO {
		return string(ProviderS3)
	}
	return string(p)
}

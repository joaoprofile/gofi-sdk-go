package cloud

import (
	"fmt"
	"sync"

	"github.com/joaoprofile/gofi/base/environment"
)

// Provider is the interface every cloud-provider implementation must satisfy.
type Provider interface {
	// Bootstrap performs the one-time connection/initialisation for the provider.
	Bootstrap() error

	// GetSession returns the underlying SDK session or client object.
	// Prefer SessionAs[T] over direct use of this method.
	GetSession() any
}

// Cloud wraps the active Provider and exposes it to the rest of the application.
type Cloud struct {
	provider Provider
}

// GetSession returns the raw session object from the active provider.
// Use SessionAs[T] for type-safe access.
func (c *Cloud) GetSession() any {
	return c.provider.GetSession()
}

var (
	initOnce sync.Once
	instance *Cloud
	initErr  error
)

// Instance returns the singleton Cloud, initialising it on the first call.
// If initialisation fails the error is sticky — subsequent calls return the
// same error without retrying.
func Instance() (*Cloud, error) {
	initOnce.Do(func() {
		cfg := environment.Instance().Cloud()
		p, err := newProvider(cfg)
		if err != nil {
			initErr = err
			return
		}
		if err := p.Bootstrap(); err != nil {
			initErr = fmt.Errorf("cloud: bootstrap failed: %w", err)
			return
		}
		instance = &Cloud{provider: p}
	})
	return instance, initErr
}

// ResetForTesting clears the singleton so that Instance() re-initialises on
// the next call. Must only be called from tests.
func ResetForTesting() {
	initOnce = sync.Once{}
	instance = nil
	initErr = nil
}

// GetSession is a package-level shortcut that returns nil when the singleton
// has not been initialised or initialisation failed.
func GetSession() any {
	if instance == nil {
		return nil
	}
	return instance.GetSession()
}

// newCloud creates a Cloud from a config without touching the singleton.
// Intended for use in tests and in Instance().
func newCloud(cfg environment.CloudConfig) (*Cloud, error) {
	p, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}
	if err := p.Bootstrap(); err != nil {
		return nil, fmt.Errorf("cloud: bootstrap failed: %w", err)
	}
	return &Cloud{provider: p}, nil
}

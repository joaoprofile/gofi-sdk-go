package cloud

import (
	"fmt"
	"sync"
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

// Init initialises the singleton Cloud from cfg on the first call and returns
// it. The result is sticky: later calls return the same instance/error without
// rebuilding, ignoring cfg. gofi's config.InitCloud populates cfg from CLOUD_*.
func Init(cfg Config) (*Cloud, error) {
	initOnce.Do(func() {
		c, err := newCloud(cfg)
		if err != nil {
			initErr = err
			return
		}
		instance = c
	})
	return instance, initErr
}

// Instance returns the singleton Cloud built by Init, or nil when Init has not
// been called (or failed).
func Instance() *Cloud {
	return instance
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
// Intended for use in tests and in Init().
func newCloud(cfg Config) (*Cloud, error) {
	p, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}
	if err := p.Bootstrap(); err != nil {
		return nil, fmt.Errorf("cloud: bootstrap failed: %w", err)
	}
	return &Cloud{provider: p}, nil
}

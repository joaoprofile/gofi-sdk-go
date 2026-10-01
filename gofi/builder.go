package gofi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

// gofiInstance is the private struct that implements both Builder and Service.
// Consumers never hold a reference to this type directly — only to the
// Builder or Service interfaces.
//
// New and With only declare what the service needs; Build performs every side
// effect (environment, logging, components) in a fixed order and undoes what
// it opened when a step fails.
type gofiInstance struct {
	serviceName string
	redactKeys  []string // extra log keys to mask (WithRedactKeys)
	env         *environment.Environment
	components  []Component
	runners     []Runner
	rt          *Runtime

	// errs collects API misuse found while chaining; Build returns it before any I/O.
	errs  []error
	built bool

	lifeMu sync.Mutex
	stop   context.CancelFunc // cancels the running ListenAndServe
	done   chan struct{}      // closed when ListenAndServe returns
	closed bool               // Shutdown was requested
}

var errNilComponent = errors.New("With: nil component")

func (g *gofiInstance) With(components ...Component) Builder {
	for _, c := range components {
		if c == nil {
			g.errs = append(g.errs, errNilComponent)
			continue
		}
		g.components = append(g.components, c)
	}
	return g
}

var errAlreadyBuilt = errors.New("gofi: Build was already called")

// buildTimeout bounds releasing resources when Build fails halfway.
const buildTimeout = 30 * time.Second

// Build loads the environment and applies process-wide settings, then starts
// the components by Stage, so the order of With calls does not matter. API
// misuse and configuration errors are returned joined before any component
// starts; if one fails, everything opened so far is closed before returning.
// In prod and stage, plaintext or unverified TLS connections are
// configuration errors (see TransportChecker).
func (g *gofiInstance) Build() (Service, error) {
	if g.built {
		return nil, errAlreadyBuilt
	}
	if err := errors.Join(g.errs...); err != nil {
		return nil, err
	}
	if err := errors.Join(g.bootstrap(), g.checkTransport()); err != nil {
		return nil, err
	}

	g.rt = NewRuntime(g.env)
	ordered := slices.Clone(g.components)
	slices.SortStableFunc(ordered, func(a, b Component) int { return cmp.Compare(a.Stage(), b.Stage()) })
	for _, c := range ordered {
		if err := c.Start(context.Background(), g.rt); err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
			defer cancel()
			return nil, errors.Join(fmt.Errorf("%s: %w", c.Name(), err), g.rt.Close(ctx))
		}
		if r, ok := c.(Runner); ok {
			g.runners = append(g.runners, r)
		}
	}

	g.built = true
	return g, nil
}

// bootstrap loads the environment and applies process-wide settings; all
// problems are returned joined.
func (g *gofiInstance) bootstrap() error {
	var errs []error
	if g.env == nil {
		g.env = environment.Instance()
		if err := environment.LoadError(); err != nil {
			errs = append(errs, fmt.Errorf("environment: %w", err))
		}
	}
	if g.serviceName != "" {
		g.env.AppName = g.serviceName
	}
	if err := core.SetTimezone(g.env); err != nil {
		errs = append(errs, fmt.Errorf("TIMEZONE: %w", err))
	}
	logCfg := core.Logging(g.env, g.env.AppName)
	logCfg.RedactKeys = g.redactKeys
	if err := logging.InitGlobal(context.Background(), logCfg); err != nil {
		errs = append(errs, fmt.Errorf("logging: %w", err))
	}
	core.ApplyTLS(g.env)
	return errors.Join(errs...)
}

// checkTransport refuses, in prod and stage, the plaintext or unverified
// connections reported by the environment and by every TransportChecker.
func (g *gofiInstance) checkTransport() error {
	found := core.InsecureTransports(g.env)
	for _, c := range g.components {
		if tc, ok := c.(TransportChecker); ok {
			found = append(found, tc.InsecureTransports(g.env)...)
		}
	}
	return core.CheckTransport(g.env, found)
}

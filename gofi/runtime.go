package gofi

import (
	"context"
	"errors"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
)

// Runtime is what Build hands to each Component.Start: the environment, the
// registry of what must be closed, the readiness checks and the resources that
// components share (one Redis client for cache and session, for instance).
type Runtime struct {
	env     *environment.Environment
	closers []func(context.Context) error
	health  []HealthCheck
	shared  map[string]any
}

// HealthCheck is a readiness check registered by a component; the HTTP server
// component serves them when its Health config is set.
type HealthCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// NewRuntime returns a Runtime for env. Build creates its own; it is exported
// so components can be started in tests without a Builder.
func NewRuntime(env *environment.Environment) *Runtime {
	return &Runtime{env: env}
}

// Env returns the environment loaded by Build.
func (r *Runtime) Env() *environment.Environment { return r.env }

// OnClose registers fn to run on Shutdown, or when a later component fails to
// start. Closers run in reverse order of registration.
func (r *Runtime) OnClose(fn func(ctx context.Context) error) {
	r.closers = append(r.closers, fn)
}

// AddHealthCheck registers a readiness check.
func (r *Runtime) AddHealthCheck(name string, check func(ctx context.Context) error) {
	r.health = append(r.health, HealthCheck{Name: name, Check: check})
}

// HealthChecks returns the checks registered so far.
func (r *Runtime) HealthChecks() []HealthCheck { return r.health }

// Shared returns the value stored under key, calling open to create it the
// first time. Components use it to share one connection; open is responsible
// for registering its closer. A failed open is not stored.
func (r *Runtime) Shared(key string, open func() (any, error)) (any, error) {
	if v, ok := r.shared[key]; ok {
		return v, nil
	}
	v, err := open()
	if err != nil {
		return nil, err
	}
	if r.shared == nil {
		r.shared = map[string]any{}
	}
	r.shared[key] = v
	return v, nil
}

// Close runs the registered closers in reverse order and forgets them.
func (r *Runtime) Close(ctx context.Context) error {
	closers := r.closers
	r.closers = nil
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		errs = append(errs, closers[i](ctx))
	}
	return errors.Join(errs...)
}

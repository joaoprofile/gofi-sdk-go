package gofi

import (
	"context"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
)

// Builder declares the components of a GOFI service. Call Build to start them
// and obtain a Service.
//
// The root package only knows the Component contract: each resource (database,
// cache, session, messaging, observability, HTTP server) lives in its own
// package under component/, so a binary links only the resources it imports.
//
//	db := database.New()
//	svc, err := gofi.New("catalog-api").
//		With(db, httpserver.New(":8080").Handlers(h)).
//		Build()
type Builder interface {
	// With adds components. The order does not matter: Build starts them by
	// Stage and closes them in reverse.
	With(components ...Component) Builder

	// Build loads the environment, sets up logging and starts every component.
	// It returns every declaration error joined; if a component fails to
	// start, the ones already started are closed and the Service is nil.
	Build() (Service, error)
}

// Service is the runtime contract of a built GOFI service. The resources are
// reached through the components themselves (db.DB(), cache.Client(), ...).
type Service interface {
	Environment() *environment.Environment

	// ListenAndServe runs the Runner components (the HTTP server) and blocks
	// until SIGINT/SIGTERM, Shutdown or a Runner failure; without Runners it
	// just waits for the signal. It then stops the Runners and closes what the
	// components opened, in reverse order. It returns nil on a clean stop.
	ListenAndServe() error

	// Shutdown stops a running ListenAndServe and waits for it to finish; when
	// the service is not running it closes the resources directly. Resources
	// injected with From* constructors are owned by the caller and not closed.
	Shutdown(ctx context.Context) error
}

// Stage orders Build: components start in ascending Stage (declaration order
// breaks ties) and are closed in reverse.
type Stage int

const (
	StageObservability Stage = 100
	StageDatabase      Stage = 200
	StageCache         Stage = 300
	StageSession       Stage = 400
	StageMessaging     Stage = 500
	StageIAM           Stage = 600
	// StageServer starts last, so it sees the health checks registered by
	// every other component.
	StageServer Stage = 900
)

// Component is a resource started by Build. Start must register in rt what it
// opens (rt.OnClose) so Shutdown and a failed Build can release it.
type Component interface {
	Name() string
	Stage() Stage
	Start(ctx context.Context, rt *Runtime) error
}

// Runner is a Component that serves until stopped, such as the HTTP server.
// ListenAndServe runs every Runner and stops them all when one returns.
type Runner interface {
	Component
	// Run blocks until the runner stops; it returns nil on a clean stop.
	Run() error
	// Stop makes Run return; it is safe to call more than once.
	Stop(ctx context.Context) error
}

// InsecureTransport is a connection that would run in plaintext or without
// verifying the server certificate.
type InsecureTransport = core.InsecureTransport

// TransportChecker is implemented by components that connect with settings
// from the environment. In prod and stage Build collects their reports before
// starting any component and fails, naming the setting to fix, unless
// GOFI_ALLOW_INSECURE_TRANSPORT lists the resource (then it only warns).
type TransportChecker interface {
	InsecureTransports(env *environment.Environment) []InsecureTransport
}

// Option customizes the service declared by New.
type Option func(*gofiInstance)

// WithRedactKeys masks the values of these log attribute keys, in addition
// to logging.DefaultRedactKeys(), in the console and in exported logs.
func WithRedactKeys(keys ...string) Option {
	return func(g *gofiInstance) { g.redactKeys = append(g.redactKeys, keys...) }
}

// New returns a Builder for the named service. It has no side effects: the
// environment, logging and every component are set up by Build.
func New(serviceName string, opts ...Option) Builder {
	g := &gofiInstance{serviceName: serviceName}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

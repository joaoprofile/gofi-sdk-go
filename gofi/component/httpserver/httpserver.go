// Package httpserver is the gofi component for the HTTP server (netx). It is a
// gofi.Runner: ListenAndServe serves it and stops it gracefully.
package httpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/netx"
)

var errNotStarted = errors.New("http server: Run before Build started it")

// Component wraps a netx.HttpServer. With New the server is built in Start,
// once the environment is known; handlers and middleware attached before
// that are replayed on it in order.
type Component struct {
	cfg     *netx.WSConfig // code config; nil with FromServer
	server  netx.HttpServer
	pending []func(netx.HttpServer)
}

// New creates the server listening on port (":8080"); cfg is optional and is
// copied. In Start its unset fields are filled from the HTTP_* environment
// (see ConfigFromEnv): code-provided values always win. The readiness checks
// of the other components are served when cfg.Health is set.
func New(port string, cfg ...*netx.WSConfig) *Component {
	config := netx.WSConfig{}
	if len(cfg) > 0 && cfg[0] != nil {
		config = *cfg[0]
	}
	config.ServerPort = port
	return &Component{cfg: &config}
}

// FromServer uses a server built by the caller; gofi serves and stops it, and
// the environment does not change it.
func FromServer(server netx.HttpServer) *Component {
	return &Component{server: server}
}

// Handlers registers the route handlers.
func (c *Component) Handlers(handlers ...netx.RouterHandler) *Component {
	c.apply(func(s netx.HttpServer) { s.AddHandlers(handlers...) })
	return c
}

// Use adds global middleware, run on every route.
func (c *Component) Use(middleware ...netx.Middleware) *Component {
	c.apply(func(s netx.HttpServer) { s.Use(middleware...) })
	return c
}

// UseAuth sets the middleware run on private routes; routes added before it
// do not get it.
func (c *Component) UseAuth(auth netx.Middleware) *Component {
	c.apply(func(s netx.HttpServer) { s.UseAuth(auth) })
	return c
}

// apply runs f on the server, or queues it until Start builds the server.
func (c *Component) apply(f func(netx.HttpServer)) {
	if c.server != nil {
		f(c.server)
		return
	}
	c.pending = append(c.pending, f)
}

// Server returns the underlying netx server: with New, nil until Build
// started the component.
func (c *Component) Server() netx.HttpServer { return c.server }

func (c *Component) Name() string      { return "http server" }
func (c *Component) Stage() gofi.Stage { return gofi.StageServer }

// Start builds the server from the code config and the environment, then
// registers the readiness checks of the components started before it.
func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	if c.server == nil {
		cfg, err := c.config(rt.Env())
		if err != nil {
			return err
		}
		c.server = netx.NewServer(cfg)
		for _, f := range c.pending {
			f(c.server)
		}
		c.pending = nil
	}
	for _, hc := range rt.HealthChecks() {
		c.server.AddHealthCheck(hc.Name, hc.Check)
	}
	return nil
}

// config merges the code config over the environment and validates it.
func (c *Component) config(env *environment.Environment) (*netx.WSConfig, error) {
	fromEnv, err := ConfigFromEnv(env)
	if err != nil {
		return nil, err
	}
	cfg := merge(*c.cfg, fromEnv, env.HTTPRateLimitFailClosed)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// InsecureTransports reports the server without TLS; in prod and stage it is
// only a warning unless HTTP_REQUIRE_TLS=true (see core.InsecureHTTP).
// Servers passed to FromServer are the caller's and are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.cfg == nil {
		return nil
	}
	tlsEnabled := c.cfg.TLS != nil || strings.TrimSpace(env.HTTPTLSCertFile) != ""
	return core.InsecureHTTP(env, tlsEnabled)
}

func (c *Component) Run() error {
	if c.server == nil {
		return errNotStarted
	}
	return c.server.ListenAndServe()
}

func (c *Component) Stop(ctx context.Context) error {
	if c.server == nil {
		return nil
	}
	return c.server.Shutdown(ctx)
}

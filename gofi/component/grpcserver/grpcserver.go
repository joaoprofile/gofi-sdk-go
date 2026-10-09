// Package grpcserver is the gofi component for the gRPC server (netx/grpcx).
// It is a gofi.Runner: ListenAndServe serves it next to the other runners and
// stops it gracefully.
//
//	grpc := grpcserver.New(":9090").Register(func(r grpc.ServiceRegistrar) {
//	    ordersv1.RegisterOrdersServer(r, ordersImpl)
//	})
//	svc, err := gofi.New("orders").With(database.New(), grpc).Build()
package grpcserver

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
)

var errNotStarted = errors.New("grpc server: Run before Build started it")

// Component wraps a grpcx.Server. With New the server is built in Start,
// once the environment is known; services registered before that are
// replayed on it in order.
type Component struct {
	cfg     *grpcx.ServerConfig // code config; nil with FromServer
	server  grpcx.Server
	pending []func(grpc.ServiceRegistrar)
}

// New creates the server listening on addr (":9090"); cfg is optional and is
// copied. In Start its unset TLS is filled from GRPC_TLS_* (see
// ConfigFromEnv): code-provided values always win.
func New(addr string, cfg ...*grpcx.ServerConfig) *Component {
	config := grpcx.ServerConfig{}
	if len(cfg) > 0 && cfg[0] != nil {
		config = *cfg[0]
	}
	config.Addr = addr
	return &Component{cfg: &config}
}

// FromServer uses a server built by the caller; gofi serves and stops it, and
// the environment does not change it.
func FromServer(server grpcx.Server) *Component {
	return &Component{server: server}
}

// Register registers services, typically with the generated
// Register*Server functions.
func (c *Component) Register(register func(r grpc.ServiceRegistrar)) *Component {
	if c.server != nil {
		register(c.server)
		return c
	}
	c.pending = append(c.pending, register)
	return c
}

// Server returns the underlying grpcx server: with New, nil until Build
// started the component.
func (c *Component) Server() grpcx.Server { return c.server }

func (c *Component) Name() string      { return "grpc server" }
func (c *Component) Stage() gofi.Stage { return gofi.StageServer }

// Start builds the server from the code config and the environment, then
// registers the readiness checks of the components started before it on the
// grpc.health.v1 service.
func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	if c.server == nil {
		cfg, err := c.config(rt.Env())
		if err != nil {
			return err
		}
		server, err := grpcx.NewServer(cfg)
		if err != nil {
			return err
		}
		c.server = server
		for _, register := range c.pending {
			register(c.server)
		}
		c.pending = nil
	}
	for _, hc := range rt.HealthChecks() {
		c.server.AddHealthCheck(hc.Name, hc.Check)
	}
	return nil
}

// config merges the code config over the environment.
func (c *Component) config(env *environment.Environment) (grpcx.ServerConfig, error) {
	cfg := *c.cfg
	if cfg.TLS == nil {
		tls, err := ConfigFromEnv(env)
		if err != nil {
			return grpcx.ServerConfig{}, err
		}
		cfg.TLS = tls
	}
	return cfg, cfg.Validate()
}

// InsecureTransports reports the server without TLS; in prod and stage it is
// only a warning unless GRPC_REQUIRE_TLS=true (see core.InsecureGRPC).
// Servers passed to FromServer are the caller's and are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.cfg == nil {
		return nil
	}
	tlsEnabled := c.cfg.TLS != nil || strings.TrimSpace(env.GRPCTLSCertFile) != ""
	return core.InsecureGRPC(env, tlsEnabled)
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

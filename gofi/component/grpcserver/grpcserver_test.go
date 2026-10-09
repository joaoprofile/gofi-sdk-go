package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/netx/grpcx"
)

var (
	_ gofi.Runner           = (*Component)(nil)
	_ gofi.TransportChecker = (*Component)(nil)
)

type mockServer struct {
	runErr    error
	health    []string
	services  []string
	shutdowns int
}

func (m *mockServer) RegisterService(desc *grpc.ServiceDesc, _ any) {
	m.services = append(m.services, desc.ServiceName)
}
func (m *mockServer) AddHealthCheck(name string, _ func(context.Context) error) {
	m.health = append(m.health, name)
}
func (m *mockServer) ListenAndServe() error          { return m.runErr }
func (m *mockServer) Serve(net.Listener) error       { return m.runErr }
func (m *mockServer) Shutdown(context.Context) error { m.shutdowns++; return nil }
func (m *mockServer) GRPC() *grpc.Server             { return nil }

func register(name string) func(grpc.ServiceRegistrar) {
	return func(r grpc.ServiceRegistrar) {
		r.RegisterService(&grpc.ServiceDesc{ServiceName: name, HandlerType: (*any)(nil)}, struct{}{})
	}
}

func TestNewCopiesConfigAndDefersServer(t *testing.T) {
	cfg := &grpcx.ServerConfig{MaxRecvMsgSize: 7}
	c := New(":9999", cfg)
	assert.Empty(t, cfg.Addr, "the caller's config is not mutated")
	assert.Equal(t, ":9999", c.cfg.Addr)
	assert.Equal(t, 7, c.cfg.MaxRecvMsgSize)
	assert.Nil(t, c.Server(), "built in Start")
	assert.NotNil(t, New(":9998").cfg, "cfg is optional")
}

func TestRegisterDelegatesToServer(t *testing.T) {
	srv := &mockServer{}
	c := FromServer(srv).Register(register("a.A")).Register(register("b.B"))
	assert.Same(t, srv, c.Server())
	assert.Equal(t, []string{"a.A", "b.B"}, srv.services)
}

func TestStartBuildsServerReplaysRegistrationsAndHealth(t *testing.T) {
	c := New("127.0.0.1:0", &grpcx.ServerConfig{DisableTelemetry: true}).Register(register("a.A"))
	require.Len(t, c.pending, 1)

	rt := gofi.NewRuntime(&environment.Environment{})
	rt.AddHealthCheck("db", func(context.Context) error { return nil })
	require.NoError(t, c.Start(context.Background(), rt))
	require.NotNil(t, c.Server())
	assert.Empty(t, c.pending)
	assert.Contains(t, c.Server().GRPC().GetServiceInfo(), "a.A")
}

func TestStartWithFromServerAddsHealthChecks(t *testing.T) {
	srv := &mockServer{}
	rt := gofi.NewRuntime(&environment.Environment{})
	rt.AddHealthCheck("cache", func(context.Context) error { return nil })
	require.NoError(t, FromServer(srv).Start(context.Background(), rt))
	assert.Equal(t, []string{"cache"}, srv.health)
}

func TestStartFailsOnInvalidEnv(t *testing.T) {
	c := New(":0")
	err := c.Start(context.Background(), gofi.NewRuntime(&environment.Environment{GRPCTLSCertFile: "/only/cert.pem"}))
	assert.Error(t, err)
}

func TestRunAndStop(t *testing.T) {
	assert.ErrorIs(t, New(":0").Run(), errNotStarted)
	assert.NoError(t, New(":0").Stop(context.Background()), "stop before start is a no-op")

	srv := &mockServer{runErr: errors.New("boom")}
	c := FromServer(srv)
	assert.EqualError(t, c.Run(), "boom")
	require.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, 1, srv.shutdowns)
}

func TestInsecureTransports(t *testing.T) {
	prod := &environment.Environment{AppEnvironment: "prod", GRPCRequireTLS: true}
	assert.Len(t, New(":0").InsecureTransports(prod), 1, "plaintext refused with GRPC_REQUIRE_TLS")

	withEnvTLS := &environment.Environment{AppEnvironment: "prod", GRPCRequireTLS: true, GRPCTLSCertFile: "/tls/c.pem"}
	assert.Empty(t, New(":0").InsecureTransports(withEnvTLS))
	assert.Empty(t, FromServer(&mockServer{}).InsecureTransports(prod), "FromServer is not checked")
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := ConfigFromEnv(&environment.Environment{})
	require.NoError(t, err)
	assert.Nil(t, cfg)

	cfg, err = ConfigFromEnv(&environment.Environment{GRPCTLSCertFile: "c", GRPCTLSKeyFile: "k", GRPCTLSClientCAFile: "ca"})
	require.NoError(t, err)
	assert.Equal(t, "ca", cfg.ClientCAFile)
	assert.Equal(t, tls.NoClientCert, cfg.ClientAuth, "netx turns a CA into require_and_verify")

	for _, env := range []*environment.Environment{
		{GRPCTLSCertFile: "c"},
		{GRPCTLSClientCAFile: "ca"},
		{GRPCTLSCertFile: "c", GRPCTLSKeyFile: "k", GRPCTLSClientAuth: "bogus"},
		{GRPCTLSCertFile: "c", GRPCTLSKeyFile: "k", GRPCTLSClientAuth: "require_and_verify"},
		{GRPCTLSCertFile: "c", GRPCTLSKeyFile: "k", GRPCTLSClientCAFile: "ca", GRPCTLSClientAuth: "none"},
	} {
		_, err := ConfigFromEnv(env)
		assert.Error(t, err, "%+v", env)
	}
}

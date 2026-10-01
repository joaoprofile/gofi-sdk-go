package httpserver

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ gofi.Runner           = (*Component)(nil)
	_ gofi.TransportChecker = (*Component)(nil)
)

type mockServer struct {
	runErr    error
	health    []string
	calls     []string
	shutdowns int
}

func (m *mockServer) ListenAndServe() error          { return m.runErr }
func (m *mockServer) Shutdown(context.Context) error { m.shutdowns++; return nil }
func (m *mockServer) AddHealthCheck(name string, _ func(context.Context) error) {
	m.health = append(m.health, name)
}
func (m *mockServer) AddHandlers(...netx.RouterHandler) { m.calls = append(m.calls, "handlers") }
func (m *mockServer) Use(...netx.Middleware)            { m.calls = append(m.calls, "use") }
func (m *mockServer) UseAuth(netx.Middleware)           { m.calls = append(m.calls, "auth") }

func TestNewCopiesConfigAndDefersServer(t *testing.T) {
	cfg := &netx.WSConfig{MaxBodyBytes: 7}
	c := New(":9999", cfg)
	assert.Empty(t, cfg.ServerPort, "the caller's config is not mutated")
	assert.Equal(t, ":9999", c.cfg.ServerPort)
	assert.Equal(t, int64(7), c.cfg.MaxBodyBytes)
	assert.Nil(t, c.Server(), "built in Start")
	assert.NotNil(t, New(":9998").cfg, "cfg is optional")
}

func TestChainingDelegatesToServer(t *testing.T) {
	srv := &mockServer{}
	c := FromServer(srv).Use(nil, nil).UseAuth(nil).Handlers(nil)
	assert.Same(t, srv, c.Server())
	assert.Equal(t, []string{"use", "auth", "handlers"}, srv.calls)
}

func TestStartBuildsServerAndReplaysChaining(t *testing.T) {
	var used []string
	mw := func(name string) netx.Middleware {
		return func(h http.Handler) http.Handler { used = append(used, name); return h }
	}
	c := New(":0").Use(mw("global")).UseAuth(mw("auth")).Handlers(routes{})
	require.Len(t, c.pending, 3)

	rt := gofi.NewRuntime(&environment.Environment{})
	rt.AddHealthCheck("database", func(context.Context) error { return nil })
	require.NoError(t, c.Start(context.Background(), rt))
	assert.NotNil(t, c.Server())
	assert.Nil(t, c.pending)

	c.Handlers() // after Start: applied directly
	assert.Nil(t, c.pending)
	assert.Contains(t, used, "auth", "UseAuth replayed before Handlers, so the private route got it")
}

type routes struct{}

func (routes) Handlers() []*netx.Route {
	return netx.PrivateRoutes("/p", netx.GET("/").To(func(http.ResponseWriter, *http.Request) {}))
}

func TestStartRejectsInvalidEnv(t *testing.T) {
	c := New(":0")
	rt := gofi.NewRuntime(&environment.Environment{HTTPTrustedProxies: "not-a-cidr"})
	assert.ErrorContains(t, c.Start(context.Background(), rt), "HTTP_TRUSTED_PROXIES")
	assert.Nil(t, c.Server())

	c = New(":0", &netx.WSConfig{AllowedOrigins: []string{"*"}})
	assert.ErrorContains(t, c.Start(context.Background(), gofi.NewRuntime(&environment.Environment{})), "AllowCredentials")
}

func TestStartRegistersHealthChecks(t *testing.T) {
	srv := &mockServer{}
	rt := gofi.NewRuntime(&environment.Environment{})
	rt.AddHealthCheck("database", func(context.Context) error { return nil })
	rt.AddHealthCheck("cache", func(context.Context) error { return nil })

	require.NoError(t, FromServer(srv).Start(context.Background(), rt))
	assert.Equal(t, []string{"database", "cache"}, srv.health)
}

func TestRunAndStopDelegateToServer(t *testing.T) {
	srv := &mockServer{runErr: errors.New("listen failed")}
	c := FromServer(srv)
	assert.ErrorContains(t, c.Run(), "listen failed")
	assert.NoError(t, c.Stop(context.Background()))
	assert.Equal(t, 1, srv.shutdowns)
}

func TestRunAndStopBeforeStart(t *testing.T) {
	c := New(":0")
	assert.ErrorIs(t, c.Run(), errNotStarted)
	assert.NoError(t, c.Stop(context.Background()))
}

func TestInsecureTransports(t *testing.T) {
	prod := &environment.Environment{AppEnvironment: "prod", HTTPRequireTLS: true}
	assert.Len(t, New(":0").InsecureTransports(prod), 1, "plaintext with HTTP_REQUIRE_TLS")
	assert.Empty(t, New(":0", &netx.WSConfig{TLS: &netx.TLSConfig{}}).InsecureTransports(prod), "TLS in code")

	withEnvTLS := *prod
	withEnvTLS.HTTPTLSCertFile = "/tls/tls.crt"
	assert.Empty(t, New(":0").InsecureTransports(&withEnvTLS), "TLS from env")

	assert.Empty(t, FromServer(&mockServer{}).InsecureTransports(prod), "caller's server")
	assert.Empty(t, New(":0").InsecureTransports(&environment.Environment{AppEnvironment: "prod"}), "only a warning")
}

func TestIdentity(t *testing.T) {
	c := New(":9997")
	assert.Equal(t, "http server", c.Name())
	assert.Equal(t, gofi.StageServer, c.Stage())
}

package grpcx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// freeAddr returns a local address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func waitServing(t *testing.T, addr string) {
	t.Helper()
	health := grpc_health_v1.NewHealthClient(dial(t, addr, ClientConfig{}))
	require.Eventually(t, func() bool {
		resp, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 10*time.Millisecond)
}

func TestServer_ListenAndServe(t *testing.T) {
	addr := freeAddr(t)
	srv, err := NewServer(ServerConfig{
		Addr:                 addr,
		DrainDelay:           10 * time.Millisecond,
		Reflection:           true,
		MaxConcurrentStreams: 16,
		MaxRecvMsgSize:       1 << 20,
		MaxSendMsgSize:       1 << 20,
	})
	require.NoError(t, err)
	srv.RegisterService(&echoDesc, echo)
	assert.Contains(t, srv.GRPC().GetServiceInfo(), "grpc.reflection.v1.ServerReflection")

	served := make(chan error, 1)
	go func() { served <- srv.ListenAndServe() }()
	waitServing(t, addr)

	got, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "traced")
	require.NoError(t, err, "the telemetry stats handler is on by default")
	assert.Equal(t, "traced", got)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
	assert.NoError(t, <-served)
}

func TestServer_ListenAndServeAddrInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	srv, err := NewServer(ServerConfig{Addr: ln.Addr().String(), DisableTelemetry: true})
	require.NoError(t, err)
	assert.ErrorContains(t, srv.ListenAndServe(), "grpcx: listen")
}

// failingListener fails Accept with a permanent error.
type failingListener struct{ net.Listener }

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }

func TestServer_ServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	srv, err := NewServer(ServerConfig{Addr: ":0", DisableTelemetry: true})
	require.NoError(t, err)
	assert.ErrorContains(t, srv.Serve(failingListener{ln}), "grpcx: serve: accept failed")
}

func TestServer_ShutdownTimeout(t *testing.T) {
	entered := make(chan struct{})
	srv, err := NewServer(ServerConfig{
		Addr:             ":0",
		DrainDelay:       -1,
		ShutdownTimeout:  200 * time.Millisecond,
		DisableTelemetry: true,
	})
	require.NoError(t, err)
	srv.RegisterService(&echoDesc, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		close(entered)
		<-ctx.Done() // only the forced stop cancels it
		return nil, ctx.Err()
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	conn := dial(t, ln.Addr().String(), ClientConfig{Timeout: -1})
	callErr := make(chan error, 1)
	go func() {
		_, err := callEcho(context.Background(), conn, "x")
		callErr <- err
	}()
	<-entered

	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, srv.Shutdown(short), context.DeadlineExceeded, "Shutdown gives up when ctx ends first")

	assert.ErrorContains(t, <-served, "graceful stop timed out")
	assert.Error(t, <-callErr, "the open call is cancelled")
}

func TestServer_DrainDelay(t *testing.T) {
	assert.Equal(t, DefaultDrainDelay, (&server{}).drainDelay())
	assert.Equal(t, time.Duration(0), (&server{config: ServerConfig{DrainDelay: -1}}).drainDelay())
	assert.Equal(t, time.Second, (&server{config: ServerConfig{DrainDelay: time.Second}}).drainDelay())
}

func TestNewServer_InvalidConfig(t *testing.T) {
	_, err := NewServer(ServerConfig{})
	assert.Error(t, err)
}

func TestServerConfig_ValidateLimits(t *testing.T) {
	assert.Error(t, ServerConfig{Addr: ":1", MaxRecvMsgSize: -1}.Validate())
	assert.Error(t, ServerConfig{Addr: ":1", Keepalive: KeepaliveConfig{Time: -1}}.Validate())
}

func TestHealth_ProbeGivesUpWhileChecksRun(t *testing.T) {
	h := newHealthService(HealthConfig{})
	h.setReady(true)
	release := make(chan struct{})
	h.addCheck("slow", func(context.Context) error { <-release; return nil })

	first := make(chan *grpc_health_v1.HealthCheckResponse, 1)
	go func() {
		resp, _ := h.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		first <- resp
	}()
	require.Eventually(t, func() bool {
		h.cacheMu.Lock()
		defer h.cacheMu.Unlock()
		return h.flight != nil
	}, time.Second, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := h.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_NOT_SERVING, resp.GetStatus(),
		"a probe whose ctx ends while waiting for the round reports NOT_SERVING")

	close(release)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, (<-first).GetStatus())
}

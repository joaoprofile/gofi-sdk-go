package grpcx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/joaoprofile/gofi-sdk-go/netx"
)

func TestClient_MutualTLS(t *testing.T) {
	certFile, keyFile := writeSelfSigned(t)
	_, addr := startServer(t, ServerConfig{TLS: &netx.TLSConfig{
		CertFile: certFile, KeyFile: keyFile, ClientCAFile: certFile,
	}}, echo)

	conn := dial(t, addr, ClientConfig{TLS: &ClientTLSConfig{
		CAFile: certFile, CertFile: certFile, KeyFile: keyFile, ServerName: "localhost",
	}})
	got, err := callEcho(context.Background(), conn, "mtls")
	require.NoError(t, err)
	assert.Equal(t, "mtls", got)

	noCert := dial(t, addr, ClientConfig{TLS: &ClientTLSConfig{CAFile: certFile, ServerName: "localhost"}})
	_, err = callEcho(context.Background(), noCert, "x")
	assert.Error(t, err, "the server requires a client certificate")
}

func TestClient_TLSConfigErrors(t *testing.T) {
	certFile, _ := writeSelfSigned(t)
	garbage := filepath.Join(t.TempDir(), "garbage.pem")
	require.NoError(t, os.WriteFile(garbage, []byte("not a certificate"), 0o600))

	for name, tlsCfg := range map[string]*ClientTLSConfig{
		"missing CA file":  {CAFile: filepath.Join(t.TempDir(), "missing.pem")},
		"CA without certs": {CAFile: garbage},
		"invalid key pair": {CertFile: certFile, KeyFile: garbage},
		"cert without key": {CertFile: certFile},
	} {
		_, err := NewClient("localhost:1", ClientConfig{TLS: tlsCfg})
		assert.Error(t, err, name)
	}

	conn, err := NewClient("localhost:1", ClientConfig{})
	require.NoError(t, err, "the zero config dials TLS against the system roots")
	require.NoError(t, conn.Close())
}

func TestClient_TokenSourceError(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echo)
	conn := dial(t, addr, ClientConfig{Token: func(context.Context) (string, error) {
		return "", errors.New("vault down")
	}})
	_, err := callEcho(context.Background(), conn, "x")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestClient_Options(t *testing.T) {
	var userAgent []string
	var seen []string
	_, addr := startServer(t, ServerConfig{UnaryInterceptors: []grpc.UnaryServerInterceptor{
		func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			userAgent = md.Get("user-agent")
			return handler(ctx, req)
		},
	}}, echo)

	conn, err := NewClient("passthrough:///"+addr, ClientConfig{
		Insecure:       true,
		LoadBalancing:  "pick_first",
		MaxRecvMsgSize: 1 << 20,
		MaxSendMsgSize: 1 << 20,
		KeepaliveTime:  time.Minute,
		UserAgent:      "orders/1.0",
		Timeout:        -1,
		Retry:          &RetryPolicy{}, // defaults: 3 attempts on Unavailable
		UnaryInterceptors: []grpc.UnaryClientInterceptor{
			func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				_, hasDeadline := ctx.Deadline()
				assert.False(t, hasDeadline, "a negative Timeout adds no deadline")
				seen = append(seen, method)
				return invoker(ctx, method, req, reply, cc, opts...)
			},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	got, err := callEcho(context.Background(), conn, "x")
	require.NoError(t, err, "the telemetry stats handler is on by default")
	assert.Equal(t, "x", got)
	assert.Equal(t, []string{echoMethod}, seen)
	require.NotEmpty(t, userAgent)
	assert.Contains(t, userAgent[0], "orders/1.0")
}

func TestClient_TimeoutKeepsCallerDeadline(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		deadline, _ := ctx.Deadline()
		return wrapperspb.String(time.Until(deadline).Round(time.Hour).String()), nil
	}))
	conn := dial(t, addr, ClientConfig{Timeout: 50 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	got, err := callEcho(ctx, conn, "x")
	require.NoError(t, err)
	assert.Equal(t, "1h0m0s", got, "the default timeout does not shorten a caller deadline")
}

func TestClient_ExplicitRequestIDWins(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		return wrapperspb.String(netx.GetRequestID(ctx)), nil
	}))
	conn := dial(t, addr, ClientConfig{})

	ctx := context.WithValue(context.Background(), netx.RequestIDKey, "from-ctx")
	ctx = metadata.AppendToOutgoingContext(ctx, RequestIDMetadata, "explicit")
	got, err := callEcho(ctx, conn, "x")
	require.NoError(t, err)
	assert.Equal(t, "explicit", got, "outgoing metadata already carrying an ID is kept")
}

func TestClient_RetryPolicyBounds(t *testing.T) {
	_, err := NewClient("localhost:1", ClientConfig{Insecure: true, Retry: &RetryPolicy{MaxAttempts: 1}})
	assert.Error(t, err)
}

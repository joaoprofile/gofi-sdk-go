package grpcx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/joaoprofile/gofi-sdk-go/base/errs"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// --- a hand-written echo service, so the tests need no generated code ---

type echoer interface {
	Echo(ctx context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
}

type echoFunc func(ctx context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error)

func (f echoFunc) Echo(ctx context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	return f(ctx, in)
}

const echoMethod = "/test.Echo/Echo"

var echoDesc = grpc.ServiceDesc{
	ServiceName: "test.Echo",
	HandlerType: (*echoer)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Echo",
		Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			in := new(wrapperspb.StringValue)
			if err := dec(in); err != nil {
				return nil, err
			}
			h := func(ctx context.Context, req any) (any, error) {
				return srv.(echoer).Echo(ctx, req.(*wrapperspb.StringValue))
			}
			if interceptor == nil {
				return h(ctx, in)
			}
			return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: echoMethod}, h)
		},
	}},
}

// startServer serves impl on a random local port and returns the address.
func startServer(t *testing.T, cfg ServerConfig, impl echoer) (Server, string) {
	t.Helper()
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	if cfg.DrainDelay == 0 {
		cfg.DrainDelay = -1
	}
	cfg.DisableTelemetry = true
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.RegisterService(&echoDesc, impl)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, srv.Shutdown(ctx))
		require.NoError(t, <-served)
	})
	return srv, ln.Addr().String()
}

func dial(t *testing.T, addr string, cfg ClientConfig) *grpc.ClientConn {
	t.Helper()
	if cfg.TLS == nil {
		cfg.Insecure = true
	}
	cfg.DisableTelemetry = true
	conn, err := NewClient("passthrough:///"+addr, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func callEcho(ctx context.Context, conn *grpc.ClientConn, msg string, opts ...grpc.CallOption) (string, error) {
	out := new(wrapperspb.StringValue)
	err := conn.Invoke(ctx, echoMethod, wrapperspb.String(msg), out, opts...)
	return out.GetValue(), err
}

var echo = echoFunc(func(_ context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	return wrapperspb.String(in.GetValue()), nil
})

func TestServer_Echo(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echo)
	got, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

var errTestNotFound = errs.RegisterNotFound("GRPCX_TEST_NOT_FOUND", "thing not found")

func TestServer_AppErrorRoundTrip(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		return nil, errTestNotFound.Wrap(errors.New("db: no rows"))
	}))
	_, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "x")

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.NotContains(t, err.Error(), "db: no rows", "the cause must not reach the caller")

	appErr, ok := FromError(err)
	require.True(t, ok)
	assert.Equal(t, "GRPCX_TEST_NOT_FOUND", appErr.Code)
	assert.Equal(t, errs.KindNotFound, appErr.Kind)
	assert.True(t, errors.Is(appErr, errTestNotFound))
}

func TestServer_PlainErrorIsInternal(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		return nil, errors.New("secret detail")
	}))
	_, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "x")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, internalMessage, status.Convert(err).Message())
}

func TestServer_RecoversFromPanic(t *testing.T) {
	calls := 0
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(_ context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		calls++
		if calls == 1 {
			panic("boom")
		}
		return in, nil
	}))
	conn := dial(t, addr, ClientConfig{})
	_, err := callEcho(context.Background(), conn, "x")
	assert.Equal(t, codes.Internal, status.Code(err))

	got, err := callEcho(context.Background(), conn, "after")
	require.NoError(t, err, "the server keeps serving after a panic")
	assert.Equal(t, "after", got)
}

type callerKey struct{}

func TestServer_BearerAuth(t *testing.T) {
	cfg := ServerConfig{Auth: &AuthConfig{Validate: func(ctx context.Context, token string) (context.Context, error) {
		if token != "good" {
			return nil, errors.New("bad token")
		}
		return context.WithValue(ctx, callerKey{}, "svc-a"), nil
	}}}
	_, addr := startServer(t, cfg, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		caller, _ := ctx.Value(callerKey{}).(string)
		return wrapperspb.String(caller), nil
	}))

	_, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "x")
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "no token")

	bad := dial(t, addr, ClientConfig{Token: func(context.Context) (string, error) { return "bad", nil }})
	_, err = callEcho(context.Background(), bad, "x")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, unauthenticatedMessage, status.Convert(err).Message(), "the validator error is not exposed")

	good := dial(t, addr, ClientConfig{Token: func(context.Context) (string, error) { return "good", nil }})
	got, err := callEcho(context.Background(), good, "x")
	require.NoError(t, err)
	assert.Equal(t, "svc-a", got, "the validator context reaches the handler")

	health := grpc_health_v1.NewHealthClient(dial(t, addr, ClientConfig{}))
	_, err = health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	assert.NoError(t, err, "health checks are always public")
}

func TestServer_PublicMethods(t *testing.T) {
	cfg := ServerConfig{Auth: &AuthConfig{
		Validate:      func(context.Context, string) (context.Context, error) { return nil, errors.New("deny") },
		PublicMethods: []string{"/test.Echo/"},
	}}
	_, addr := startServer(t, cfg, echo)
	_, err := callEcho(context.Background(), dial(t, addr, ClientConfig{}), "x")
	assert.NoError(t, err)
}

func TestServer_RequestIDPropagation(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		return wrapperspb.String(netx.GetRequestID(ctx)), nil
	}))
	conn := dial(t, addr, ClientConfig{})

	ctx := context.WithValue(context.Background(), netx.RequestIDKey, "req-123")
	var trailer metadata.MD
	got, err := callEcho(ctx, conn, "x", grpc.Trailer(&trailer))
	require.NoError(t, err)
	assert.Equal(t, "req-123", got, "the client forwards the request ID of ctx")
	assert.Equal(t, []string{"req-123"}, trailer.Get(RequestIDMetadata), "the server echoes it")

	got, err = callEcho(context.Background(), conn, "x")
	require.NoError(t, err)
	assert.NotEmpty(t, got, "a missing ID is generated")

	bad := metadata.AppendToOutgoingContext(context.Background(), RequestIDMetadata, "bad id with spaces")
	got, err = callEcho(bad, conn, "x")
	require.NoError(t, err)
	assert.NotEqual(t, "bad id with spaces", got, "an invalid ID is replaced")
}

func TestServer_HealthChecks(t *testing.T) {
	srv, addr := startServer(t, ServerConfig{Health: HealthConfig{CacheTTL: time.Millisecond}}, echo)
	health := grpc_health_v1.NewHealthClient(dial(t, addr, ClientConfig{}))

	resp, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())

	srv.AddHealthCheck("db", func(context.Context) error { return errors.New("down") })
	resp, err = health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_NOT_SERVING, resp.GetStatus())

	_, err = health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "other"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestClient_DefaultTimeout(t *testing.T) {
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(ctx context.Context, _ *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	conn := dial(t, addr, ClientConfig{Timeout: 50 * time.Millisecond})
	_, err := callEcho(context.Background(), conn, "x")
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestClient_RetryUnavailable(t *testing.T) {
	calls := 0
	_, addr := startServer(t, ServerConfig{}, echoFunc(func(_ context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
		calls++
		if calls < 3 {
			return nil, status.Error(codes.Unavailable, "try again")
		}
		return in, nil
	}))
	conn := dial(t, addr, ClientConfig{Retry: &RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond}})
	got, err := callEcho(context.Background(), conn, "x")
	require.NoError(t, err)
	assert.Equal(t, "x", got)
	assert.Equal(t, 3, calls)
}

func TestServer_TLS(t *testing.T) {
	certFile, keyFile := writeSelfSigned(t)
	_, addr := startServer(t, ServerConfig{TLS: &netx.TLSConfig{CertFile: certFile, KeyFile: keyFile}}, echo)

	conn := dial(t, addr, ClientConfig{TLS: &ClientTLSConfig{CAFile: certFile, ServerName: "localhost"}})
	got, err := callEcho(context.Background(), conn, "secure")
	require.NoError(t, err)
	assert.Equal(t, "secure", got)

	plain := dial(t, addr, ClientConfig{})
	_, err = callEcho(context.Background(), plain, "x")
	assert.Error(t, err, "a plaintext client cannot talk to a TLS server")
}

func TestServer_ShutdownBeforeServe(t *testing.T) {
	srv, err := NewServer(ServerConfig{Addr: "127.0.0.1:0", DisableTelemetry: true})
	require.NoError(t, err)
	require.NoError(t, srv.Shutdown(context.Background()))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	assert.NoError(t, srv.Serve(ln), "serving after Shutdown returns at once")
}

func TestServerConfig_Validate(t *testing.T) {
	assert.Error(t, ServerConfig{}.Validate(), "Addr is required")
	assert.Error(t, ServerConfig{Addr: ":1", Auth: &AuthConfig{}}.Validate(), "Auth needs Validate")
	assert.Error(t, ServerConfig{Addr: ":1", TLS: &netx.TLSConfig{CertFile: "missing.pem"}}.Validate())
	assert.NoError(t, ServerConfig{Addr: ":1"}.Validate())
}

func TestClientConfig_Errors(t *testing.T) {
	_, err := NewClient("", ClientConfig{Insecure: true})
	assert.Error(t, err)
	_, err = NewClient("localhost:1", ClientConfig{Insecure: true, TLS: &ClientTLSConfig{}})
	assert.Error(t, err, "Insecure and TLS are exclusive")
	_, err = NewClient("localhost:1", ClientConfig{Insecure: true, Retry: &RetryPolicy{MaxAttempts: 9}})
	assert.Error(t, err)
	_, err = NewClient("localhost:1", ClientConfig{Insecure: true, Retry: &RetryPolicy{Codes: []codes.Code{codes.OK}}})
	assert.Error(t, err)
	_, err = NewClient("localhost:1", ClientConfig{TLS: &ClientTLSConfig{CertFile: "only-cert.pem"}})
	assert.Error(t, err)
}

func TestStatus(t *testing.T) {
	assert.NoError(t, Status(nil))
	assert.NoError(t, Status(errs.AppError{}), "an empty AppError is success")
	assert.Equal(t, codes.Canceled, status.Code(Status(context.Canceled)))
	assert.Equal(t, codes.DeadlineExceeded, status.Code(Status(context.DeadlineExceeded)))
	already := status.Error(codes.Aborted, "x")
	assert.Equal(t, already, Status(already), "status errors pass through")

	for kind, code := range map[errs.ErrorKind]codes.Code{
		errs.KindValidation: codes.InvalidArgument, errs.KindNotFound: codes.NotFound,
		errs.KindConflict: codes.AlreadyExists, errs.KindUnauthorized: codes.Unauthenticated,
		errs.KindForbidden: codes.PermissionDenied, errs.KindExternalError: codes.Unavailable,
		errs.KindOperation: codes.Internal, errs.KindUnknown: codes.Internal,
	} {
		assert.Equal(t, code, status.Code(Status(errs.AppError{Kind: kind, Code: "C"})), kind)
	}
}

func TestFromError(t *testing.T) {
	_, ok := FromError(nil)
	assert.False(t, ok)

	e, ok := FromError(status.Error(codes.PermissionDenied, "no"))
	require.True(t, ok)
	assert.Equal(t, errs.KindForbidden, e.Kind)

	unknown := Status(errs.AppError{Kind: errs.KindConflict, Code: "ONLY_ON_SERVER", Message: "taken"})
	e, ok = FromError(unknown)
	require.True(t, ok)
	assert.Equal(t, "ONLY_ON_SERVER", e.Code)
	assert.Equal(t, errs.KindConflict, e.Kind)
	assert.Equal(t, "taken", e.Message)
}

// writeSelfSigned writes a self-signed certificate for localhost and returns
// the PEM paths; the certificate doubles as its own CA.
func writeSelfSigned(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certFile, keyFile
}

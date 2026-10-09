package grpcx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// Client defaults applied when the matching ClientConfig field is <= 0.
const (
	// DefaultCallTimeout is the deadline of calls whose context has none.
	DefaultCallTimeout = 10 * time.Second
	// DefaultClientKeepaliveTime is above the server's DefaultKeepaliveMinTime.
	DefaultClientKeepaliveTime    = 30 * time.Second
	DefaultClientKeepaliveTimeout = 10 * time.Second
	// DefaultLoadBalancing spreads calls over every address the resolver
	// returns ("dns:///svc.ns.svc.cluster.local:9090" with a headless
	// Service reaches every pod).
	DefaultLoadBalancing = "round_robin"
)

// ClientConfig configures NewClient. The zero value dials with TLS against
// the system roots.
type ClientConfig struct {
	// TLS customizes the server verification and enables mTLS. Nil with
	// Insecure false means TLS against the system roots.
	TLS *ClientTLSConfig

	// Insecure dials in plaintext (h2c). It must be set explicitly and is
	// meant only for private networks or a mesh that encrypts the hop.
	Insecure bool

	// Token sends "authorization: Bearer <token>" on every call. Over an
	// Insecure connection the token travels in plaintext.
	Token TokenSource

	// Timeout is applied to calls whose context has no deadline
	// (default DefaultCallTimeout; negative disables it).
	Timeout time.Duration

	// Retry enables gRPC retries for the listed codes. Off by default:
	// retrying is only safe for idempotent methods.
	Retry *RetryPolicy

	// LoadBalancing is the policy name (default DefaultLoadBalancing).
	LoadBalancing string

	// MaxRecvMsgSize and MaxSendMsgSize bound a single message
	// (default DefaultMaxMsgSize).
	MaxRecvMsgSize int
	MaxSendMsgSize int

	// KeepaliveTime and KeepaliveTimeout tune client pings (defaults above).
	KeepaliveTime    time.Duration
	KeepaliveTimeout time.Duration

	// UserAgent is prepended to gRPC's own user agent.
	UserAgent string

	// UnaryInterceptors and StreamInterceptors run after the built-in ones
	// (request ID, default timeout).
	UnaryInterceptors  []grpc.UnaryClientInterceptor
	StreamInterceptors []grpc.StreamClientInterceptor

	// DisableTelemetry turns off the OpenTelemetry stats handler.
	DisableTelemetry bool
}

// ClientTLSConfig verifies the server and optionally presents a client
// certificate (mTLS).
type ClientTLSConfig struct {
	// CAFile is a PEM bundle of CAs trusted for the server; empty uses the
	// system roots.
	CAFile string
	// CertFile and KeyFile are the client certificate for mTLS (both or neither).
	CertFile string
	KeyFile  string
	// ServerName overrides the name verified in the server certificate.
	ServerName string
}

// RetryPolicy is the gRPC retry policy applied to every method of the
// connection.
type RetryPolicy struct {
	// MaxAttempts counts the first call (default 3, gRPC caps it at 5).
	MaxAttempts int
	// InitialBackoff and MaxBackoff bound the jittered exponential backoff
	// (defaults 100ms and 2s).
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// Codes are the retryable status codes (default Unavailable).
	Codes []codes.Code
}

// NewClient creates a client connection to target ("dns:///host:port"). The
// connection is lazy: it connects on the first call. The caller closes it.
func NewClient(target string, cfg ClientConfig) (*grpc.ClientConn, error) {
	if strings.TrimSpace(target) == "" {
		return nil, errors.New("grpcx: target is required")
	}
	opts, err := clientOptions(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpcx: client %s: %w", target, err)
	}
	return conn, nil
}

func clientOptions(c ClientConfig) ([]grpc.DialOption, error) {
	if c.Insecure && c.TLS != nil {
		return nil, errors.New("grpcx: Insecure and TLS are mutually exclusive")
	}
	creds, err := transportCredentials(c)
	if err != nil {
		return nil, err
	}
	serviceConfig, err := clientServiceConfig(c)
	if err != nil {
		return nil, err
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultServiceConfig(serviceConfig),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(orDefaultInt(c.MaxRecvMsgSize, DefaultMaxMsgSize)),
			grpc.MaxCallSendMsgSize(orDefaultInt(c.MaxSendMsgSize, DefaultMaxMsgSize)),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    orDefault(c.KeepaliveTime, DefaultClientKeepaliveTime),
			Timeout: orDefault(c.KeepaliveTimeout, DefaultClientKeepaliveTimeout),
		}),
		grpc.WithChainUnaryInterceptor(append([]grpc.UnaryClientInterceptor{
			requestIDClientUnary, timeoutClientUnary(c.Timeout),
		}, c.UnaryInterceptors...)...),
		grpc.WithChainStreamInterceptor(append([]grpc.StreamClientInterceptor{
			requestIDClientStream,
		}, c.StreamInterceptors...)...),
	}
	if c.Token != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(BearerCredentials(c.Token, !c.Insecure)))
	}
	if c.UserAgent != "" {
		opts = append(opts, grpc.WithUserAgent(c.UserAgent))
	}
	if !c.DisableTelemetry {
		opts = append(opts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}
	return opts, nil
}

func transportCredentials(c ClientConfig) (credentials.TransportCredentials, error) {
	if c.Insecure {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if t := c.TLS; t != nil {
		tlsCfg.ServerName = t.ServerName
		if t.CAFile != "" {
			pem, err := os.ReadFile(t.CAFile) // #nosec G304 -- operator-configured CA bundle
			if err != nil {
				return nil, fmt.Errorf("grpcx: read CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("grpcx: no certificate found in CA file %s", t.CAFile)
			}
			tlsCfg.RootCAs = pool
		}
		if (t.CertFile == "") != (t.KeyFile == "") {
			return nil, errors.New("grpcx: client CertFile and KeyFile must be set together")
		}
		if t.CertFile != "" {
			pair, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("grpcx: load client certificate: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{pair}
		}
	}
	return credentials.NewTLS(tlsCfg), nil
}

// clientServiceConfig renders the default service config: load balancing and,
// when enabled, the retry policy for every method.
func clientServiceConfig(c ClientConfig) (string, error) {
	type retry struct {
		MaxAttempts          int      `json:"maxAttempts"`
		InitialBackoff       string   `json:"initialBackoff"`
		MaxBackoff           string   `json:"maxBackoff"`
		BackoffMultiplier    float64  `json:"backoffMultiplier"`
		RetryableStatusCodes []string `json:"retryableStatusCodes"`
	}
	type methodConfig struct {
		Name        []map[string]string `json:"name"`
		RetryPolicy *retry              `json:"retryPolicy,omitempty"`
	}
	sc := struct {
		LoadBalancingConfig []map[string]struct{} `json:"loadBalancingConfig"`
		MethodConfig        []methodConfig        `json:"methodConfig,omitempty"`
	}{
		LoadBalancingConfig: []map[string]struct{}{{cmpOr(c.LoadBalancing, DefaultLoadBalancing): {}}},
	}
	if r := c.Retry; r != nil {
		attempts := r.MaxAttempts
		if attempts <= 0 {
			attempts = 3
		}
		if attempts < 2 || attempts > 5 {
			return "", fmt.Errorf("grpcx: Retry.MaxAttempts=%d: want 2..5", attempts)
		}
		retryCodes := r.Codes
		if len(retryCodes) == 0 {
			retryCodes = []codes.Code{codes.Unavailable}
		}
		names := make([]string, 0, len(retryCodes))
		for _, code := range retryCodes {
			name, ok := codeNames[code]
			if !ok {
				return "", fmt.Errorf("grpcx: Retry.Codes: %v is not retryable", code)
			}
			names = append(names, name)
		}
		sc.MethodConfig = []methodConfig{{
			Name: []map[string]string{{}},
			RetryPolicy: &retry{
				MaxAttempts:          attempts,
				InitialBackoff:       seconds(orDefault(r.InitialBackoff, 100*time.Millisecond)),
				MaxBackoff:           seconds(orDefault(r.MaxBackoff, 2*time.Second)),
				BackoffMultiplier:    2,
				RetryableStatusCodes: names,
			},
		}}
	}
	out, err := json.Marshal(sc)
	if err != nil {
		return "", fmt.Errorf("grpcx: service config: %w", err)
	}
	return string(out), nil
}

// codeNames are the status code names gRPC service configs expect.
var codeNames = map[codes.Code]string{
	codes.Canceled: "CANCELLED", codes.Unknown: "UNKNOWN", codes.InvalidArgument: "INVALID_ARGUMENT",
	codes.DeadlineExceeded: "DEADLINE_EXCEEDED", codes.NotFound: "NOT_FOUND",
	codes.AlreadyExists: "ALREADY_EXISTS", codes.PermissionDenied: "PERMISSION_DENIED",
	codes.ResourceExhausted: "RESOURCE_EXHAUSTED", codes.FailedPrecondition: "FAILED_PRECONDITION",
	codes.Aborted: "ABORTED", codes.OutOfRange: "OUT_OF_RANGE", codes.Unimplemented: "UNIMPLEMENTED",
	codes.Internal: "INTERNAL", codes.Unavailable: "UNAVAILABLE", codes.DataLoss: "DATA_LOSS",
	codes.Unauthenticated: "UNAUTHENTICATED",
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%gs", d.Seconds())
}

func cmpOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// requestIDClientUnary forwards the request ID of ctx (netx.GetRequestID) so
// a call made while serving an HTTP or gRPC request keeps its ID downstream.
func requestIDClientUnary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(outgoingRequestID(ctx), method, req, reply, cc, opts...)
}

func requestIDClientStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(outgoingRequestID(ctx), desc, cc, method, opts...)
}

func outgoingRequestID(ctx context.Context) context.Context {
	id := netx.GetRequestID(ctx)
	if !validRequestID(id) {
		return ctx
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok && len(md.Get(RequestIDMetadata)) > 0 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, RequestIDMetadata, id)
}

// timeoutClientUnary bounds calls whose context has no deadline.
func timeoutClientUnary(timeout time.Duration) grpc.UnaryClientInterceptor {
	if timeout < 0 {
		return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
	}
	timeout = orDefault(timeout, DefaultCallTimeout)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

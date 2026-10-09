package grpcx

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/netx"
	"google.golang.org/grpc"
)

// Server defaults applied when the matching ServerConfig field is <= 0.
const (
	DefaultMaxMsgSize        = 4 << 20 // 4 MiB, gRPC's own receive default
	DefaultConnectionTimeout = 10 * time.Second
	// DefaultShutdownTimeout stays below Kubernetes' default 30s grace period.
	DefaultShutdownTimeout = 25 * time.Second
	// DefaultDrainDelay lets endpoints observe NOT_SERVING before the
	// listener closes.
	DefaultDrainDelay = 5 * time.Second

	// Keepalive defaults. The server accepts client pings every
	// DefaultKeepaliveMinTime, below the client's DefaultClientKeepaliveTime,
	// so grpcx clients are never disconnected for pinging too often.
	DefaultKeepaliveTime      = 2 * time.Minute
	DefaultKeepaliveTimeout   = 20 * time.Second
	DefaultKeepaliveMinTime   = 15 * time.Second
	DefaultMaxConnectionIdle  = 15 * time.Minute
	DefaultMaxConnectionGrace = 30 * time.Second
)

// ServerConfig configures NewServer. The zero value serves plaintext h2c on
// Addr with the defaults above.
type ServerConfig struct {
	// Addr is the listen address (":9090").
	Addr string

	// TLS enables TLS (and mTLS with ClientCAFile) with the same hardening
	// and certificate reload as the netx HTTP server. ALPN is h2 only.
	TLS *netx.TLSConfig

	// MaxRecvMsgSize and MaxSendMsgSize bound a single message
	// (default DefaultMaxMsgSize).
	MaxRecvMsgSize int
	MaxSendMsgSize int

	// MaxConcurrentStreams bounds the streams per connection (0 = gRPC default).
	MaxConcurrentStreams uint32

	// ConnectionTimeout bounds the connection handshake (TLS included).
	ConnectionTimeout time.Duration

	// Keepalive tunes server pings and connection ageing.
	Keepalive KeepaliveConfig

	// ShutdownTimeout bounds the graceful stop; streams still open after it
	// are cancelled.
	ShutdownTimeout time.Duration

	// DrainDelay is waited between reporting NOT_SERVING and closing the
	// listener (default DefaultDrainDelay; negative disables it).
	DrainDelay time.Duration

	// Health tunes the grpc.health.v1 service, which is always registered.
	Health HealthConfig

	// Reflection registers the server reflection service. Off by default: it
	// lists every service and method to anyone who can reach the port.
	Reflection bool

	// Auth validates a bearer token on every call except the public methods.
	// Health checks are always public.
	Auth *AuthConfig

	// UnaryInterceptors and StreamInterceptors run after the built-in ones
	// (recovery, request ID, logging, auth, error mapping), closest to the
	// handler.
	UnaryInterceptors  []grpc.UnaryServerInterceptor
	StreamInterceptors []grpc.StreamServerInterceptor

	// DisableTelemetry turns off the OpenTelemetry stats handler.
	DisableTelemetry bool
}

// KeepaliveConfig tunes server keepalive; zero fields use the defaults.
type KeepaliveConfig struct {
	// Time is the idle period after which the server pings the client.
	Time time.Duration
	// Timeout is how long the server waits for the ping ack.
	Timeout time.Duration
	// MinClientInterval is the shortest client ping interval accepted;
	// clients pinging faster are disconnected.
	MinClientInterval time.Duration
	// MaxConnectionIdle closes connections idle for this long.
	MaxConnectionIdle time.Duration
	// MaxConnectionAge closes connections after this age so clients
	// rebalance across replicas (0 = never).
	MaxConnectionAge time.Duration
	// MaxConnectionAgeGrace lets calls in flight finish after MaxConnectionAge.
	MaxConnectionAgeGrace time.Duration
}

// HealthConfig tunes the grpc.health.v1 service.
type HealthConfig struct {
	// CheckTimeout bounds all readiness checks of one probe (default 2s).
	CheckTimeout time.Duration
	// CacheTTL reuses the last result (default 1s); concurrent probes share
	// one round of checks.
	CacheTTL time.Duration
}

// Validate reports a config NewServer cannot serve.
func (c ServerConfig) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, errors.New("grpcx: Addr is required"))
	}
	if c.MaxRecvMsgSize < 0 || c.MaxSendMsgSize < 0 {
		errs = append(errs, errors.New("grpcx: message size limits must be >= 0"))
	}
	if c.Auth != nil && c.Auth.Validate == nil {
		errs = append(errs, errors.New("grpcx: Auth.Validate is required"))
	}
	k := c.Keepalive
	if k.Time < 0 || k.Timeout < 0 || k.MinClientInterval < 0 || k.MaxConnectionIdle < 0 ||
		k.MaxConnectionAge < 0 || k.MaxConnectionAgeGrace < 0 {
		errs = append(errs, errors.New("grpcx: keepalive durations must be >= 0"))
	}
	if c.TLS != nil {
		if _, err := netx.ServerTLSConfig(c.TLS); err != nil {
			errs = append(errs, fmt.Errorf("grpcx: %w", err))
		}
	}
	return errors.Join(errs...)
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

func orDefaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

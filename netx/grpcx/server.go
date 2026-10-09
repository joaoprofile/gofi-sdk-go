package grpcx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/stats"

	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// Server is a gRPC server with the httpx server lifecycle. It is a
// grpc.ServiceRegistrar, so generated Register*Server functions accept it.
type Server interface {
	grpc.ServiceRegistrar
	// AddHealthCheck registers a readiness check served by grpc.health.v1.
	AddHealthCheck(name string, check func(ctx context.Context) error)
	// ListenAndServe listens on Addr and serves until SIGINT/SIGTERM or
	// Shutdown, then reports NOT_SERVING, drains and stops gracefully. It
	// returns nil on a clean stop.
	ListenAndServe() error
	// Serve is ListenAndServe on a listener the caller opened, without
	// signal handling.
	Serve(ln net.Listener) error
	// Shutdown stops a running ListenAndServe/Serve and waits for it, or for
	// ctx. Calling it before serving makes the next serve return at once.
	Shutdown(ctx context.Context) error
	// GRPC returns the underlying *grpc.Server, for features grpcx does not
	// wrap.
	GRPC() *grpc.Server
}

type server struct {
	config ServerConfig
	grpc   *grpc.Server
	health *healthService

	lifeMu sync.Mutex
	stop   context.CancelFunc
	done   chan struct{}
	closed bool
}

// NewServer builds the server; it does not listen until ListenAndServe.
func NewServer(config ServerConfig) (Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	opts, err := serverOptions(config)
	if err != nil {
		return nil, err
	}
	s := &server{
		config: config,
		grpc:   grpc.NewServer(opts...),
		health: newHealthService(config.Health),
	}
	grpc_health_v1.RegisterHealthServer(s.grpc, s.health)
	if config.Reflection {
		reflection.Register(s.grpc)
	}
	return s, nil
}

// serverOptions assembles transport, limits, keepalive, telemetry and the
// built-in interceptor chain.
func serverOptions(c ServerConfig) ([]grpc.ServerOption, error) {
	k := c.Keepalive
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(orDefaultInt(c.MaxRecvMsgSize, DefaultMaxMsgSize)),
		grpc.MaxSendMsgSize(orDefaultInt(c.MaxSendMsgSize, DefaultMaxMsgSize)),
		grpc.ConnectionTimeout(orDefault(c.ConnectionTimeout, DefaultConnectionTimeout)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  orDefault(k.Time, DefaultKeepaliveTime),
			Timeout:               orDefault(k.Timeout, DefaultKeepaliveTimeout),
			MaxConnectionIdle:     orDefault(k.MaxConnectionIdle, DefaultMaxConnectionIdle),
			MaxConnectionAge:      k.MaxConnectionAge, // 0 = infinity in gRPC
			MaxConnectionAgeGrace: orDefault(k.MaxConnectionAgeGrace, DefaultMaxConnectionGrace),
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             orDefault(k.MinClientInterval, DefaultKeepaliveMinTime),
			PermitWithoutStream: true,
		}),
		grpc.ChainUnaryInterceptor(unaryChain(c)...),
		grpc.ChainStreamInterceptor(streamChain(c)...),
	}
	if c.MaxConcurrentStreams > 0 {
		opts = append(opts, grpc.MaxConcurrentStreams(c.MaxConcurrentStreams))
	}
	if !c.DisableTelemetry {
		opts = append(opts, grpc.StatsHandler(serverStatsHandler()))
	}
	if c.TLS != nil {
		tlsCfg, err := netx.ServerTLSConfig(c.TLS)
		if err != nil {
			return nil, fmt.Errorf("grpcx: %w", err)
		}
		tlsCfg = tlsCfg.Clone()
		tlsCfg.NextProtos = []string{"h2"}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}
	return opts, nil
}

// serverStatsHandler traces and measures every RPC except health checks.
func serverStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler(otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
		return !isHealthMethod(info.FullMethodName)
	}))
}

func (s *server) RegisterService(desc *grpc.ServiceDesc, impl any) {
	s.grpc.RegisterService(desc, impl)
}

func (s *server) GRPC() *grpc.Server { return s.grpc }

func (s *server) AddHealthCheck(name string, check func(ctx context.Context) error) {
	s.health.addCheck(name, check)
}

func (s *server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		return fmt.Errorf("grpcx: listen %s: %w", s.config.Addr, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.run(ctx, stop, ln)
}

func (s *server) Serve(ln net.Listener) error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	return s.run(ctx, stop, ln)
}

func (s *server) run(ctx context.Context, stop context.CancelFunc, ln net.Listener) error {
	s.lifeMu.Lock()
	if s.closed {
		s.lifeMu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.stop, s.done = stop, make(chan struct{})
	done := s.done
	s.lifeMu.Unlock()
	defer close(done)

	return s.serve(ctx, ln)
}

func (s *server) Shutdown(ctx context.Context) error {
	s.lifeMu.Lock()
	s.closed = true
	stop, done := s.stop, s.done
	s.lifeMu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serve runs until ctx is done, then reports NOT_SERVING, waits DrainDelay
// and stops gracefully within ShutdownTimeout.
func (s *server) serve(ctx context.Context, ln net.Listener) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.grpc.Serve(ln) }()
	s.health.setReady(true)
	slog.InfoContext(ctx, "gRPC service started", slog.String("addr", ln.Addr().String()), slog.Bool("tls", s.config.TLS != nil))

	select {
	case err := <-serveErr:
		s.health.setReady(false)
		if err == nil || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return fmt.Errorf("grpcx: serve: %w", err)
	case <-ctx.Done():
	}

	s.health.setReady(false)
	if d := s.drainDelay(); d > 0 {
		slog.Info("gRPC service draining", slog.Duration("delay", d))
		time.Sleep(d)
	}

	stopped := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopped)
	}()
	timer := time.NewTimer(orDefault(s.config.ShutdownTimeout, DefaultShutdownTimeout))
	defer timer.Stop()
	select {
	case <-stopped:
		slog.Info("gRPC service stopped gracefully")
		return nil
	case <-timer.C:
		s.grpc.Stop() // cancels the streams still open
		<-stopped
		return errors.New("grpcx: shutdown: graceful stop timed out; open streams were cancelled")
	}
}

func (s *server) drainDelay() time.Duration {
	switch {
	case s.config.DrainDelay < 0:
		return 0
	case s.config.DrainDelay > 0:
		return s.config.DrainDelay
	default:
		return DefaultDrainDelay
	}
}

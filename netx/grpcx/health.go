package grpcx

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// Readiness defaults.
const (
	defaultCheckTimeout      = 2 * time.Second
	defaultReadinessCacheTTL = time.Second
)

const healthServicePrefix = "/grpc.health.v1.Health/"

func isHealthMethod(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, healthServicePrefix)
}

// healthService answers grpc.health.v1 Check for the whole server (empty
// service name): SERVING while the server is up and every readiness check
// passes. Failed checks are logged, never returned, since the probe is
// usually unauthenticated. Watch is not implemented (Kubernetes uses Check).
type healthService struct {
	grpc_health_v1.UnimplementedHealthServer

	cfg   HealthConfig
	ready atomic.Bool

	checksMu sync.RWMutex
	checks   map[string]func(ctx context.Context) error

	cacheMu sync.Mutex
	at      time.Time
	ok      bool
	flight  chan struct{} // closed when the round in progress ends
}

func newHealthService(cfg HealthConfig) *healthService {
	return &healthService{cfg: cfg}
}

func (h *healthService) setReady(v bool) { h.ready.Store(v) }

func (h *healthService) addCheck(name string, check func(ctx context.Context) error) {
	h.checksMu.Lock()
	if h.checks == nil {
		h.checks = make(map[string]func(ctx context.Context) error)
	}
	h.checks[name] = check
	h.checksMu.Unlock()

	h.cacheMu.Lock()
	h.at = time.Time{} // the cached result does not cover the new check
	h.cacheMu.Unlock()
}

func (h *healthService) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if req.GetService() != "" {
		return nil, status.Error(codes.NotFound, "unknown service")
	}
	if !h.ready.Load() || !h.result(ctx) {
		return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, nil
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// result returns the cached outcome or runs one round of checks shared by
// every concurrent caller.
func (h *healthService) result(ctx context.Context) bool {
	ttl := orDefault(h.cfg.CacheTTL, defaultReadinessCacheTTL)
	for {
		h.cacheMu.Lock()
		if !h.at.IsZero() && time.Since(h.at) < ttl {
			ok := h.ok
			h.cacheMu.Unlock()
			return ok
		}
		if wait := h.flight; wait != nil {
			h.cacheMu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return false
			}
		}
		h.flight = make(chan struct{})
		h.cacheMu.Unlock()

		ok := h.runChecks(ctx)

		h.cacheMu.Lock()
		h.ok, h.at = ok, time.Now()
		close(h.flight)
		h.flight = nil
		h.cacheMu.Unlock()
		return ok
	}
}

func (h *healthService) runChecks(ctx context.Context) bool {
	h.checksMu.RLock()
	checks := maps.Clone(h.checks)
	h.checksMu.RUnlock()
	if len(checks) == 0 {
		return true
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orDefault(h.cfg.CheckTimeout, defaultCheckTimeout))
	defer cancel()

	var (
		wg     sync.WaitGroup
		failed atomic.Bool
	)
	for name, check := range checks {
		wg.Go(func() {
			if err := check(ctx); err != nil {
				failed.Store(true)
				logging.FromContext(ctx).Warn("readiness check failed", slog.String("check", name), slog.Any("error", err))
			}
		})
	}
	wg.Wait()
	return !failed.Load()
}

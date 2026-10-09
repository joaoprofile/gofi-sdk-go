package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HealthConfig enables liveness and readiness endpoints. They are served ahead
// of every middleware, so probes never hit auth, rate limiting or logging.
type HealthConfig struct {
	LivenessPath  string        // default "/livez"
	ReadinessPath string        // default "/readyz"
	CheckTimeout  time.Duration // default 2s for all readiness checks
	// CacheTTL reuses the last readiness result (default 1s); concurrent
	// probes share one round of checks, so /readyz cannot flood dependencies.
	CacheTTL time.Duration
}

// Readiness defaults.
const (
	defaultCheckTimeout      = 2 * time.Second
	defaultReadinessCacheTTL = time.Second
)

// AddHealthCheck registers a readiness check; it is ignored when Health is nil.
func (ws *httpServer) AddHealthCheck(name string, check func(ctx context.Context) error) {
	ws.checksMu.Lock()
	defer ws.checksMu.Unlock()
	if ws.checks == nil {
		ws.checks = make(map[string]func(ctx context.Context) error)
	}
	ws.checks[name] = check

	ws.readiness.mu.Lock()
	ws.readiness.at = time.Time{} // the cached result does not cover the new check
	ws.readiness.mu.Unlock()
}

// handler puts the probes in front of the router when Health is enabled.
func (ws *httpServer) handler() http.Handler {
	h := ws.config.Health
	if h == nil {
		return ws.router
	}
	live := orDefaultString(h.LivenessPath, "/livez")
	ready := orDefaultString(h.ReadinessPath, "/readyz")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == live:
			writeProbe(w, http.StatusOK, map[string]string{"status": "ok"})
		case r.Method == http.MethodGet && r.URL.Path == ready:
			ws.serveReadiness(w, r)
		default:
			ws.router.ServeHTTP(w, r)
		}
	})
}

// serveReadiness answers only {check: "fail"} per failed check; the errors
// are logged, never returned, since the probe is usually public.
func (ws *httpServer) serveReadiness(w http.ResponseWriter, r *http.Request) {
	if !ws.ready.Load() {
		writeProbe(w, http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		return
	}
	if failed := ws.readinessResult(r.Context()); len(failed) > 0 {
		writeProbe(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "checks": failed})
		return
	}
	writeProbe(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readinessCache holds the last round of checks and the running one.
type readinessCache struct {
	mu      sync.Mutex
	at      time.Time
	failed  map[string]string
	pending chan struct{} // closed when the running round ends
}

// readinessResult returns the cached failures while fresh; otherwise it runs
// one round of checks, which concurrent callers wait for and share.
func (ws *httpServer) readinessResult(ctx context.Context) map[string]string {
	rc := &ws.readiness
	rc.mu.Lock()
	if !rc.at.IsZero() && time.Since(rc.at) < orDefault(ws.config.Health.CacheTTL, defaultReadinessCacheTTL) {
		defer rc.mu.Unlock()
		return rc.failed
	}
	if wait := rc.pending; wait != nil {
		rc.mu.Unlock()
		<-wait // bounded by CheckTimeout
		rc.mu.Lock()
		defer rc.mu.Unlock()
		return rc.failed
	}
	done := make(chan struct{})
	rc.pending = done
	rc.mu.Unlock()

	failed := ws.runChecks(ctx)

	rc.mu.Lock()
	rc.failed, rc.at, rc.pending = failed, time.Now(), nil
	rc.mu.Unlock()
	close(done)
	return failed
}

// runChecks runs every check concurrently and returns the failed ones; a
// check still running at CheckTimeout counts as failed.
func (ws *httpServer) runChecks(parent context.Context) map[string]string {
	ws.checksMu.RLock()
	checks := make(map[string]func(context.Context) error, len(ws.checks))
	maps.Copy(checks, ws.checks)
	ws.checksMu.RUnlock()

	// Detached from the probe: a disconnecting client must not fail a shared round.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), orDefault(ws.config.Health.CheckTimeout, defaultCheckTimeout))
	defer cancel()

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(checks))
	for name, check := range checks {
		go func() { results <- result{name, safeCheck(ctx, check)} }()
	}

	failed := map[string]string{}
	for range len(checks) {
		select {
		case res := <-results:
			delete(checks, res.name)
			if res.err != nil {
				failed[res.name] = "fail"
				logCheckFailure(parent, res.name, res.err)
			}
		case <-ctx.Done():
			for name := range checks {
				failed[name] = "fail"
				logCheckFailure(parent, name, ctx.Err())
			}
			return failed
		}
	}
	return failed
}

// safeCheck runs check, turning a panic into an error.
func safeCheck(ctx context.Context, check func(context.Context) error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("readiness check panicked: %v", rec)
		}
	}()
	return check(ctx)
}

func logCheckFailure(ctx context.Context, name string, err error) {
	logging.FromContext(ctx).Warn("readiness check failed", slog.String("check", name), slog.Any("error", err))
}

func writeProbe(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func orDefaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// instrumented wraps h with OpenTelemetry spans and http.server metrics; health
// probes are excluded so they do not flood traces. The span starts before
// routing, so it is named "HTTP <method>" until routeTelemetry renames it.
func (ws *httpServer) instrumented(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool { return !ws.isProbe(r.URL.Path) }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return "HTTP " + r.Method }),
	)
}

func (ws *httpServer) isProbe(path string) bool {
	h := ws.config.Health
	if h == nil {
		return false
	}
	return path == orDefaultString(h.LivenessPath, "/livez") || path == orDefaultString(h.ReadinessPath, "/readyz")
}

// routeTelemetry names the server span after the matched route ("GET
// /orders/{id}") and adds http.route to it and to the http.server.* metrics,
// through the otelhttp labeler. The pattern keeps cardinality bounded: path
// parameters stay as placeholders.
func routeTelemetry(method, pattern string) Middleware {
	route := attribute.String("http.route", pattern)
	name := method + " " + pattern
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetName(name)
			span.SetAttributes(route)
			if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
				labeler.Add(route)
			}
			next.ServeHTTP(w, r)
		})
	}
}

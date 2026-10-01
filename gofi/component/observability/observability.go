// Package observability is the gofi component that exports traces, metrics
// and logs over OTLP/gRPC. It is the only component that links the
// OpenTelemetry SDK and gRPC; services that do not import it carry neither.
package observability

import (
	"context"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
	"github.com/gofi-labs/gofi-sdk-go/obs"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

// Component starts OpenTelemetry for the service.
type Component struct {
	tele     *obs.Telemetry
	injected bool
}

// New configures telemetry from OTEL_* (see ConfigFromEnv). When
// OTEL_EXPORTER_OTLP_ENDPOINT is empty, Start skips it with a warning.
func New() *Component { return &Component{} }

// FromTelemetry uses telemetry initialized by the caller, who owns and shuts
// it down.
func FromTelemetry(t *obs.Telemetry) *Component {
	return &Component{tele: t, injected: true}
}

func (c *Component) Name() string      { return "observability" }
func (c *Component) Stage() gofi.Stage { return gofi.StageObservability }

func (c *Component) Start(ctx context.Context, rt *gofi.Runtime) error {
	if c.injected {
		return nil
	}
	cfg := ConfigFromEnv(rt.Env())
	if cfg.CollectorAddr == "" {
		logging.Warn("observability: OTEL_EXPORTER_OTLP_ENDPOINT is not set, skipping")
		return nil
	}
	if err := validateTLS(cfg); err != nil {
		return err
	}
	tele, err := obs.Init(ctx, cfg)
	if err != nil {
		return err
	}
	c.tele = tele
	rt.OnClose(tele.Shutdown)
	return nil
}

// InsecureTransports reports a plaintext OTLP export to a collector that is
// not on the loopback interface or a unix socket. Injected telemetry is not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.injected {
		return nil
	}
	cfg := ConfigFromEnv(env)
	addr := cfg.CollectorAddr
	if cfg.TLS || core.IsLoopback(addr) || strings.HasPrefix(addr, "unix:") {
		return nil
	}
	return []gofi.InsecureTransport{{
		Resource: core.ResourceOTLP,
		Setting:  "OTEL_EXPORTER_OTLP_INSECURE / OTEL_EXPORTER_OTLP_ENDPOINT",
		Detail:   "collector " + addr + " without TLS",
	}}
}

// Telemetry returns the running telemetry; nil when it was skipped.
func (c *Component) Telemetry() *obs.Telemetry { return c.tele }

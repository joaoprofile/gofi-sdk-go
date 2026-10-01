package observability

import (
	"errors"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/obs"
)

// ConfigFromEnv builds obs.TeleConfig from the environment: the service
// identity (APP_NAME / APP_VERSION / APP_ENVIRONMENT) and
// OTEL_EXPORTER_OTLP_ENDPOINT. Without APP_VERSION, service.version comes from
// OTEL_RESOURCE_ATTRIBUTES or is "unknown".
// A zero-value CollectorAddr means telemetry is not configured.
//
// As the OTel spec defines for OTLP/gRPC, the exporter uses TLS unless the
// endpoint scheme is http:// or, without a scheme,
// OTEL_EXPORTER_OTLP_INSECURE=true.
//
// OTEL_EXPORTER_OTLP_CERTIFICATE (CA bundle, replaces the system roots) and
// OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE / OTEL_EXPORTER_OTLP_CLIENT_KEY (mTLS)
// map to CAFile, CertFile and KeyFile. The signal-specific variants
// (OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_*) are not supported: all signals
// share one connection. Start rejects TLS files with a plaintext endpoint.
func ConfigFromEnv(env *environment.Environment) obs.TeleConfig {
	addr, insecure := otlpEndpoint(env)
	return obs.TeleConfig{
		ServiceName:       env.AppName,
		ServiceVersion:    env.AppVersion,
		ServiceEnv:        env.AppEnvironment,
		CollectorAddr:     addr,
		TLS:               !insecure,
		CAFile:            strings.TrimSpace(env.OtelExporterOTLPCertificate),
		CertFile:          strings.TrimSpace(env.OtelExporterOTLPClientCertificate),
		KeyFile:           strings.TrimSpace(env.OtelExporterOTLPClientKey),
		LegacyMetricNames: env.OtelLegacyMetricNames,
	}
}

// validateTLS reports OTLP TLS files that cannot be used, naming the
// variables involved.
func validateTLS(cfg obs.TeleConfig) error {
	files := cfg.CAFile != "" || cfg.CertFile != "" || cfg.KeyFile != ""
	switch {
	case files && !cfg.TLS:
		return errors.New("observability: OTEL_EXPORTER_OTLP_CERTIFICATE / OTEL_EXPORTER_OTLP_CLIENT_* need a TLS endpoint (not http:// nor OTEL_EXPORTER_OTLP_INSECURE=true)")
	case (cfg.CertFile == "") != (cfg.KeyFile == ""):
		return errors.New("observability: OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE and OTEL_EXPORTER_OTLP_CLIENT_KEY must be set together")
	}
	return nil
}

// otlpEndpoint returns the gRPC target and whether the connection is
// plaintext. An http(s):// endpoint becomes host:port; other gRPC targets
// (dns:///, unix://) are kept as they are.
func otlpEndpoint(env *environment.Environment) (addr string, insecure bool) {
	addr = strings.TrimSpace(env.OtelExporterOTLPEndpoint)
	insecure = strings.EqualFold(strings.TrimSpace(env.OtelExporterOTLPInsecure), "true")
	scheme, rest, ok := strings.Cut(addr, "://")
	if !ok || (!strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https")) {
		return addr, insecure
	}
	host, _, _ := strings.Cut(rest, "/")
	return host, strings.EqualFold(scheme, "http")
}

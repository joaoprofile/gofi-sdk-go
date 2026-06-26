package config

import (
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/obs"
)

// Observability builds obs.TeleConfig from the environment: the service
// identity (APP_NAME / APP_ENVIRONMENT) and OTEL_EXPORTER_OTLP_ENDPOINT.
// A zero-value CollectorAddr means telemetry is not configured.
func Observability(env *environment.Environment) obs.TeleConfig {
	return obs.TeleConfig{
		ServiceName:   env.AppName,
		ServiceEnv:    env.AppEnvironment,
		CollectorAddr: env.OtelExporterOTLPEndpoint,
	}
}

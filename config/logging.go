package config

import (
	"context"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/obs/logging"
)

// Logging builds a logging.Config from the environment for the given service.
func Logging(env *environment.Environment, serviceName string) logging.Config {
	return logging.Config{
		ServiceName:   serviceName,
		Environment:   string(env.GetEnvironmentType()),
		Level:         logging.SlogLevel(env.GetLogLevel()),
		CollectorAddr: env.OtelExporterOTLPEndpoint,
	}
}

// InitLogging initialises the global logger from the environment. This is the
// env-driven entry point services should use; logging.NewLogger is a
// default-only convenience for tests.
func InitLogging(env *environment.Environment, serviceName string) error {
	return logging.InitGlobal(context.Background(), Logging(env, serviceName))
}

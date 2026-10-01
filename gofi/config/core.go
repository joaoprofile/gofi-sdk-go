package config

import (
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/base/timezone"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// The process-wide settings live in config/core, which gofi's Build imports
// without the rest of this package; these wrappers keep the config API.

// Logging builds a logging.Config from the environment for the given service.
func Logging(env *environment.Environment, serviceName string) logging.Config {
	return core.Logging(env, serviceName)
}

// InitLogging initialises the global logger from the environment.
func InitLogging(env *environment.Environment, serviceName string) error {
	return core.InitLogging(env, serviceName)
}

// Timezone builds a timezone.Config from the TIMEZONE variable.
func Timezone(env *environment.Environment) timezone.Config { return core.Timezone(env) }

// SetTimezone applies the process-wide local timezone (time.Local) from the environment.
func SetTimezone(env *environment.Environment) error { return core.SetTimezone(env) }

// ApplyTLS applies TLS_INSECURE_SKIP_VERIFY to http.DefaultTransport.
func ApplyTLS(env *environment.Environment) { core.ApplyTLS(env) }

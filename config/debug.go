package config

import (
	"github.com/joaoprofile/gofi/base/debug"
	"github.com/joaoprofile/gofi/base/environment"
)

// Debug builds a debug.Config from the SERVICE_DEBUG_* variables.
func Debug(env *environment.Environment) debug.Config {
	return debug.Config{
		Addr: env.ServiceDebugAddr,
		User: env.ServiceDebugUser,
		Pass: env.ServiceDebugPass,
	}
}

// StartDebug starts the pprof debug server in the background when SERVICE_DEBUG
// is enabled; otherwise it is a no-op.
func StartDebug(env *environment.Environment) {
	if !env.ServiceDebug {
		return
	}
	debug.Start(Debug(env))
}

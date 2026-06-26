package config

import (
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/sqln/cache"
)

// ConfigureCache wires the sqln Redis cache from CACHE_* and namespaces keys
// with APP_NAME. Call it at startup before the first cache access.
func ConfigureCache(env *environment.Environment) {
	cache.Configure(cache.Config{
		URI:      env.CacheURI,
		Password: env.CachePassword,
		Prefix:   env.AppName,
	})
}

package cache

import (
	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/sqln/cache"
)

// Configure wires the sqln Redis cache from CACHE_* and namespaces keys
// with APP_NAME. Call it at startup before the first cache access.
func Configure(env *environment.Environment) {
	cache.Configure(cache.Config{
		URI:      env.CacheURI,
		Password: env.CachePassword,
		TLS:      env.CacheUseTLS,
		Prefix:   env.AppName,
	})
}

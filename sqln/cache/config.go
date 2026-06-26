package cache

// Config holds the Redis connection settings and key namespace for the cache.
// gofi's config package populates it from CACHE_* and APP_NAME; tests call
// Configure directly. Set it before the first cache access.
type Config struct {
	URI      string
	Password string
	// Prefix namespaces every key as "<Prefix>::<name>".
	Prefix string
}

// cfg is the package-level cache configuration consumed by NewCacheRedis and
// key prefixing. It is intentionally not read from the environment so the sqln
// module stays decoupled from base/environment.
var cfg Config

// Configure sets the package-level cache configuration. Call it before the
// first cache access (NewCacheRedis / InstanceRedis / any Cache operation).
func Configure(c Config) {
	cfg = c
}

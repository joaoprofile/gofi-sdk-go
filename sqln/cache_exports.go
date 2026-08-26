package sqln

import (
	"time"

	"github.com/joaoprofile/gofi/sqln/cache"
	"github.com/redis/go-redis/v9"
)

// Cache type alias for backward compatibility.
type Cache[T any] = cache.Cache[T]

// NewCache re-exported from cache/ for backward compatibility.
func NewCache[T any](name string, ttl time.Duration) *Cache[T] {
	return cache.NewCache[T](name, ttl)
}

// InstanceRedis re-exported from cache/ for backward compatibility.
func InstanceRedis() redis.UniversalClient {
	return cache.InstanceRedis()
}

// NewCacheRedis re-exported from cache/ for backward compatibility.
func NewCacheRedis() {
	cache.NewCacheRedis()
}

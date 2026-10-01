package sqln

import (
	"context"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/cache"
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

// PingRedis checks the shared cache client.
func PingRedis(ctx context.Context) error { return cache.Ping(ctx) }

// CloseRedis closes the shared cache client created by InstanceRedis.
func CloseRedis() error { return cache.Close() }

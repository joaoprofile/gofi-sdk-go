package cache

import (
	"context"
	"crypto/tls"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gofi-labs/gofi-sdk-go/base/observer"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
	"github.com/redis/go-redis/v9"
)

// shared is the process-wide client. It is created lazily from Configure's
// settings or injected with UseClient; access is lock-free for readers.
type shared struct {
	client    redis.UniversalClient
	owned     bool // created here, so Close releases it
	closeOnce sync.Once
	closeErr  error
}

var (
	current atomic.Pointer[shared]
	dialMu  sync.Mutex
)

// InstanceRedis returns the shared client, creating it on first use. After
// Close it keeps returning the closed client (commands fail with
// redis.ErrClosed) instead of silently dialing again.
func InstanceRedis() redis.UniversalClient {
	if s := current.Load(); s != nil {
		return s.client
	}
	dialMu.Lock()
	defer dialMu.Unlock()
	if s := current.Load(); s != nil {
		return s.client
	}
	s := &shared{client: redis.NewUniversalClient(redisOptions(cfg)), owned: true}
	current.Store(s)
	logging.Debug("cache: redis client created")
	return s.client
}

// UseClient makes the cache reuse an existing Redis client instead of dialing
// its own; the caller keeps ownership of the client. nil resets the cache.
func UseClient(client redis.UniversalClient) {
	if client == nil {
		current.Store(nil)
		return
	}
	current.Store(&shared{client: client})
}

// NewCacheRedis creates the shared client and logs when Redis is unreachable.
// Prefer Ping, which returns the error.
func NewCacheRedis() {
	if err := Ping(context.Background()); err != nil {
		logging.Error("cache: redis unreachable", slog.Any("error", err))
	}
}

// Ping checks the shared client, creating it when needed.
func Ping(ctx context.Context) error {
	return InstanceRedis().Ping(ctx).Err()
}

// Close waits for work tracked by observer.GetWaitGroup, then closes the
// client created by InstanceRedis; injected clients belong to the caller.
// It is idempotent.
func Close() error {
	s := current.Load()
	if s == nil || !s.owned {
		return nil
	}
	s.closeOnce.Do(func() {
		if observer.WaitRunningTimeout() {
			logging.Warn("cache: in-flight work timed out, closing redis anyway")
		}
		s.closeErr = s.client.Close()
	})
	return s.closeErr
}

func redisOptions(c Config) *redis.UniversalOptions {
	opts := &redis.UniversalOptions{Addrs: []string{c.URI}, Password: c.Password}
	if c.TLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return opts
}

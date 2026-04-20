package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisDriver implements Driver using a Redis backend.
// Use NewRedisDriver to construct one; it satisfies the Driver interface
// so it can be passed directly to New.
type RedisDriver struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisDriver returns a Driver backed by the given Redis client.
// ttl is used as the fallback when an Entry has already expired at save time
// or when acquiring a lock with ttl=0.
func NewRedisDriver(client redis.UniversalClient, ttl time.Duration) Driver {
	return &RedisDriver{client: client, ttl: ttl}
}

func (r *RedisDriver) Save(ctx context.Context, key string, entry *Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	ttl := entry.TTL()
	if ttl <= 0 {
		ttl = r.ttl
	}

	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *RedisDriver) Get(ctx context.Context, key string) (*Entry, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var entry Entry
	if err := json.Unmarshal([]byte(val), &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (r *RedisDriver) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisDriver) ScanAll(ctx context.Context, prefix string) ([]string, error) {
	var cursor uint64
	var keys []string
	pattern := fmt.Sprintf("%s*", prefix)

	for {
		batch, next, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

func (r *RedisDriver) CleanExpired(ctx context.Context, prefix string) error {
	keys, err := r.ScanAll(ctx, prefix)
	if err != nil {
		return err
	}

	for _, key := range keys {
		entry, err := r.Get(ctx, key)
		if err != nil || entry == nil {
			continue
		}
		if entry.IsExpired() {
			_ = r.client.Del(ctx, key).Err()
		}
	}

	return nil
}

func (r *RedisDriver) lockKey(key string) string {
	return "lock:" + key
}

func (r *RedisDriver) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = r.ttl
	}
	return r.client.SetNX(ctx, r.lockKey(key), "locked", ttl).Result()
}

func (r *RedisDriver) ReleaseLock(ctx context.Context, key string) error {
	return r.client.Del(ctx, r.lockKey(key)).Err()
}

func (r *RedisDriver) IsLocked(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, r.lockKey(key)).Result()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

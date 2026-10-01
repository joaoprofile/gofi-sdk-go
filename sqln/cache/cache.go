package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache[T any] struct {
	name   string
	ttl    time.Duration
	client redis.UniversalClient // nil uses the shared client
}

func NewCache[T any](name string, ttl time.Duration) *Cache[T] {
	return &Cache[T]{name: name, ttl: ttl}
}

// WithClient points this cache at its own Redis client instead of the shared
// one; the caller keeps ownership of the client.
func (c *Cache[T]) WithClient(client redis.UniversalClient) *Cache[T] {
	c.client = client
	return c
}

func (c *Cache[T]) redis() redis.UniversalClient {
	if c.client != nil {
		return c.client
	}
	return InstanceRedis()
}

func (c *Cache[T]) List(ctx context.Context) ([]T, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	result, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	var list []T
	if err = json.Unmarshal(result, &list); err != nil {
		return nil, err
	}

	return list, nil
}

func (c *Cache[T]) UniqueResult(ctx context.Context) (*T, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	result, err := c.get(ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	model := new(T)
	if err = json.Unmarshal(result, &model); err != nil {
		return nil, err
	}

	return model, nil
}

// Get unmarshals the cached entry into dest (a non-nil pointer). Returns
// (true, nil) on hit, (false, nil) on miss, (false, err) on infra/unmarshal
// error. Use when the cached shape isn't T or []T — e.g. a *Page[T] from a
// paginated query.
func (c *Cache[T]) Get(ctx context.Context, dest any) (bool, error) {
	if err := c.validate(); err != nil {
		return false, err
	}

	result, err := c.get(ctx)
	if err != nil {
		return false, err
	}
	if result == nil {
		return false, nil
	}

	if err = json.Unmarshal(result, dest); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Cache[T]) Set(ctx context.Context, data any) error {
	if err := c.validate(); err != nil {
		return err
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return c.set(ctx, jsonData)
}

func (c *Cache[T]) Del(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}

	return c.del(ctx)
}

func (c *Cache[T]) validate() error {
	if c.client == nil && current.Load() == nil {
		return errors.New("Cache not initialized")
	}
	if c.name == "" {
		return errors.New("Cache without name")
	}
	return nil
}

func (c *Cache[T]) getNamePrefixed() string {
	return fmt.Sprintf("%s::%s", cfg.Prefix, c.name)
}

func (c *Cache[T]) get(ctx context.Context) ([]byte, error) {
	return c.getBytes(ctx, c.getNamePrefixed())
}

func (c *Cache[T]) getBytes(ctx context.Context, key string) ([]byte, error) {
	result, err := c.redis().Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return result, err
}

func (c *Cache[T]) set(ctx context.Context, data []byte) error {
	err := c.redis().Set(ctx, c.getNamePrefixed(), data, c.ttl).Err()
	return err
}

// del removes the base entry and every keyed entry registered in the index.
func (c *Cache[T]) del(ctx context.Context) error {
	client := c.redis()
	if err := client.Del(ctx, c.getNamePrefixed()).Err(); err != nil {
		return err
	}
	keys, err := client.SMembers(ctx, c.indexKey()).Result()
	if err != nil {
		return err
	}
	return client.Del(ctx, append(keys, c.indexKey())...).Err()
}

// GetKeyed reads the entry stored for key (e.g. a query hash) into dest.
// Returns (true, nil) on hit and (false, nil) on miss.
func (c *Cache[T]) GetKeyed(ctx context.Context, key string, dest any) (bool, error) {
	if err := c.validate(); err != nil {
		return false, err
	}
	result, err := c.getBytes(ctx, c.keyedKey(key))
	if err != nil || result == nil {
		return false, err
	}
	if err := json.Unmarshal(result, dest); err != nil {
		return false, err
	}
	return true, nil
}

// SetKeyed stores data under key; Del removes it together with the base entry.
func (c *Cache[T]) SetKeyed(ctx context.Context, key string, data any) error {
	if err := c.validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	k := c.keyedKey(key)
	_, err = c.redis().TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, k, payload, c.ttl)
		pipe.SAdd(ctx, c.indexKey(), k)
		if c.ttl > 0 {
			pipe.Expire(ctx, c.indexKey(), c.ttl)
		}
		return nil
	})
	return err
}

// Keyed entries and their index share a hash tag so they live in one Redis Cluster slot.
func (c *Cache[T]) keyedKey(key string) string { return "{" + c.getNamePrefixed() + "}::q:" + key }
func (c *Cache[T]) indexKey() string           { return "{" + c.getNamePrefixed() + "}::keys" }

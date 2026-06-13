package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
)

const errRedisNil = "redis: nil"

type Cache[T any] struct {
	name string
	ttl  time.Duration
}

func NewCache[T any](name string, ttl time.Duration) *Cache[T] {
	return &Cache[T]{name, ttl}
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
	if redisInstance == nil {
		return errors.New("Cache not initialized")
	}
	if c.name == "" {
		return errors.New("Cache without name")
	}
	return nil
}

func (c *Cache[T]) getNamePrefixed() string {
	return fmt.Sprintf("%s::%s", environment.Instance().AppName, c.name)
}

func (c *Cache[T]) get(ctx context.Context) ([]byte, error) {
	result, err := InstanceRedis().Get(ctx, c.getNamePrefixed()).Bytes()
	if err != nil {
		if err.Error() == errRedisNil {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

func (c *Cache[T]) set(ctx context.Context, data []byte) error {
	err := InstanceRedis().Set(ctx, c.getNamePrefixed(), data, c.ttl).Err()
	return err
}

func (c *Cache[T]) del(ctx context.Context) error {
	err := InstanceRedis().Del(ctx, c.getNamePrefixed()).Err()
	return err
}

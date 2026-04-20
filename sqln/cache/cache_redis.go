package cache

import (
	"context"
	"log/slog"
	"sync"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/observer"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/redis/go-redis/v9"
)

type singletonRedis struct {
	client redis.UniversalClient
	once   sync.Once
}

var redisInstance *singletonRedis

func InstanceRedis() redis.UniversalClient {
	if redisInstance == nil || redisInstance.client == nil {
		NewCacheRedis()
	}
	return redisInstance.client
}

func NewCacheRedis() {
	if redisInstance == nil {
		redisInstance = &singletonRedis{}
	}

	redisInstance.once.Do(func() {
		env := environment.Instance()

		opts := &redis.UniversalOptions{
			Addrs:    []string{env.CacheURI},
			Password: env.CachePassword,
		}

		client := redis.NewUniversalClient(opts)

		if _, err := client.Ping(context.Background()).Result(); err != nil {
			logging.Error("An error occurred while trying to connect to the cache database. Error", slog.Any("error", err))
		}

		redisInstance.client = client

		observer.Attach(&cacheDBObserver{})
		logging.Info("Cache database connected")
	})
}

type cacheDBObserver struct{}

func (o *cacheDBObserver) Close() {
	logging.Info("Waiting to safely close the cache connection")
	if observer.WaitRunningTimeout() {
		logging.Warn("WaitGroup timed out, forcing close of the cache connection")
	}

	logging.Info("Closing cache connection")
	client := InstanceRedis()
	if err := client.Close(); err != nil {
		logging.Error("Error when closing cache connection", slog.Any("error", err))
	}

	redisInstance = nil
}

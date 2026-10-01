// Package cache is the gofi component for the shared Redis client used by the
// sqln query cache and by the session component.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	sqlncache "github.com/joaoprofile/gofi-sdk-go/sqln/cache"
	"github.com/redis/go-redis/v9"
)

// sharedKey stores the Redis client in the Runtime, so cache and session share it.
const sharedKey = "gofi/component/cache.redis"

// pingTimeout bounds the reachability check done by Build.
const pingTimeout = 5 * time.Second

// Component opens the shared Redis client.
type Component struct {
	client   redis.UniversalClient
	injected bool
}

// New opens the shared Redis client from CACHE_* during Build; an unreachable
// Redis fails Build instead of degrading silently.
func New() *Component { return &Component{} }

// FromClient uses a client created by the caller, who owns and closes it. The
// session component reuses it; the sqln query cache keeps its own client.
func FromClient(client redis.UniversalClient) *Component {
	return &Component{client: client, injected: true}
}

func (c *Component) Name() string      { return "cache" }
func (c *Component) Stage() gofi.Stage { return gofi.StageCache }

func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	Configure(rt.Env())
	if c.injected {
		if c.client == nil {
			return nil
		}
		client := c.client
		_, err := rt.Shared(sharedKey, func() (any, error) {
			rt.AddHealthCheck("cache", func(ctx context.Context) error { return client.Ping(ctx).Err() })
			return client, nil
		})
		return err
	}
	client, err := Shared(rt)
	if err != nil {
		return err
	}
	c.client = client
	return nil
}

// InsecureTransports reports CACHE_USE_TLS=false towards a non-loopback
// Redis. Injected clients are the caller's and are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.injected {
		return nil
	}
	return core.InsecureCache(env)
}

// Client returns the Redis client; nil before Build.
func (c *Component) Client() redis.UniversalClient { return c.client }

// Shared returns the Redis client of this Build: the one injected with
// FromClient or, otherwise, the sqln shared client, pinged once and closed on
// Shutdown. Components that need Redis (session) call it from Start.
func Shared(rt *gofi.Runtime) (redis.UniversalClient, error) {
	v, err := rt.Shared(sharedKey, func() (any, error) {
		env := rt.Env()
		Configure(env)
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		defer cancel()
		if err := sqlncache.Ping(ctx); err != nil {
			return nil, fmt.Errorf("redis %s: %w", env.CacheURI, err)
		}
		client := sqlncache.InstanceRedis()
		rt.OnClose(func(context.Context) error { return sqlncache.Close() })
		rt.AddHealthCheck("cache", func(ctx context.Context) error { return client.Ping(ctx).Err() })
		return client, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(redis.UniversalClient), nil
}

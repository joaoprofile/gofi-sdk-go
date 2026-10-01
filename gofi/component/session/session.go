// Package session is the gofi component that installs the global session
// store (base/session) backed by the driver selected by CACHE_TYPE.
package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	basesession "github.com/gofi-labs/gofi-sdk-go/base/session"
	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/cache"
	"github.com/gofi-labs/gofi-sdk-go/gofi/config/core"
)

// Component installs session.Instance(). Distributed locking comes with the
// driver, so there is no separate locker component.
type Component struct {
	cfg *basesession.Config
}

// New uses cfg, or basesession.DefaultSessionConfig when it is omitted or nil.
// With CACHE_TYPE=redis it reuses the cache component's Redis client, opening
// the shared one when the service has no cache component.
func New(cfg ...*basesession.Config) *Component {
	c := basesession.DefaultSessionConfig()
	if len(cfg) > 0 && cfg[0] != nil {
		c = cfg[0]
	}
	return &Component{cfg: c}
}

func (c *Component) Name() string      { return "session" }
func (c *Component) Stage() gofi.Stage { return gofi.StageSession }

func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	switch cacheType := rt.Env().CacheType; cacheType {
	case string(environment.REDIS_CACHE):
		client, err := cache.Shared(rt)
		if err != nil {
			return err
		}
		basesession.New(basesession.NewRedisDriver(client, c.cfg.TTL), c.cfg)
		return nil
	case string(environment.OCI_CACHE):
		return errors.New("OCI session driver is not implemented")
	default:
		return fmt.Errorf("unsupported CACHE_TYPE %q", cacheType)
	}
}

// InsecureTransports reports the CACHE_* Redis in plaintext when
// CACHE_TYPE=redis (see cache.Component).
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if env.GetCacheType() != environment.REDIS_CACHE {
		return nil
	}
	return core.InsecureCache(env)
}

// Config returns the session configuration in use.
func (c *Component) Config() *basesession.Config { return c.cfg }

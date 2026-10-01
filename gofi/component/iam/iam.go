// Package iam is the gofi component for the identity service (iam): JWT
// tokens, sessions with real revocation and RBAC, configured from the
// environment. The application supplies its users and tenants as ports.
package iam

import (
	"context"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config"
	configcore "github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	iamsdk "github.com/joaoprofile/gofi-sdk-go/iam"
	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// Config holds what the environment cannot provide. Every field is optional:
// without User and Tenant the service only validates tokens and revokes
// sessions (a resource server); with them it also logs users in, and
// SelectTenant requires the ticket returned by Authenticate.
type Config struct {
	User    port.UserPort
	Tenant  port.TenantPort
	RBAC    port.RBACPort
	OnEvent func(ctx context.Context, event types.IAMEvent)

	// Configure adjusts the configuration read from the environment before
	// the service is built, e.g. security settings or extra IDPs.
	Configure func(*iamconfig.DefaultConfig)
}

// Component builds the iam service during Build.
type Component struct {
	cfg      Config
	svc      *core.IAMService
	injected bool
}

// New builds the service with iam.NewDefault from the environment (see
// config.IAM): JWT_SECRET (required, 32+ bytes), JWT_ISSUER, ACCESS_TOKEN_TTL,
// REFRESH_TOKEN_TTL, JWT key rotation, OAUTH_GOOGLE_*, IAM_LOGIN_MAX_ATTEMPTS,
// IAM_LOGIN_LOCKOUT and, with CACHE_TYPE=redis, Redis sessions, login
// throttling and single-use tenant tickets. Without Redis that state is kept
// in memory: a warning in dev/test, an error in stage/prod.
func New(cfg ...Config) *Component {
	var c Config
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &Component{cfg: c}
}

// FromService uses a service built by the caller with iam.New.
func FromService(svc *core.IAMService) *Component {
	return &Component{svc: svc, injected: true}
}

func (c *Component) Name() string      { return "iam" }
func (c *Component) Stage() gofi.Stage { return gofi.StageIAM }

func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	if c.injected {
		return nil
	}
	env := rt.Env()
	if err := env.RequireAuth(); err != nil {
		return err
	}
	inMemory, err := config.IAMInMemory(env)
	if err != nil {
		return err
	}
	if inMemory {
		logging.Warn("iam: CACHE_TYPE is not redis; sessions, login throttling and tenant tickets are kept in memory (single instance only)")
	}
	dc := config.IAM(env)
	dc.User, dc.Tenant, dc.RBAC, dc.OnEvent = c.cfg.User, c.cfg.Tenant, c.cfg.RBAC, c.cfg.OnEvent
	if c.cfg.Configure != nil {
		c.cfg.Configure(&dc)
	}
	svc, err := iamsdk.NewDefault(dc)
	if err != nil {
		return err
	}
	c.svc = svc
	return nil
}

// InsecureTransports reports the Redis session store in plaintext when
// CACHE_TYPE=redis. Injected services are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.injected || env.GetCacheType() != environment.REDIS_CACHE {
		return nil
	}
	return configcore.InsecureCache(env)
}

// Service returns the iam service; nil before Build.
func (c *Component) Service() *core.IAMService { return c.svc }

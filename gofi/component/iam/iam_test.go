package iam

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config"
	configcore "github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	iamsdk "github.com/joaoprofile/gofi-sdk-go/iam"
	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/memory"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "a-32-byte-secret-key-for-testing!"

// directory is a single-user UserPort and TenantPort; any password is valid.
type directory struct{}

var user = &types.User{ID: "u1", Email: "a@b.c", Active: true}

func (directory) FindByID(context.Context, string) (*types.User, error)    { return user, nil }
func (directory) FindByEmail(context.Context, string) (*types.User, error) { return user, nil }
func (directory) ValidatePassword(context.Context, string, string) error   { return nil }
func (directory) FindOrCreateByExternalIdentity(context.Context, types.ExternalIdentity) (*types.User, error) {
	return user, nil
}
func (directory) ListUserTenants(context.Context, string) ([]types.TenantAccess, error) {
	return []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"admin"}}}, nil
}
func (directory) AssertAccess(context.Context, string, string, string) error { return nil }

func TestStartRequiresJWTSecret(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{})
	assert.ErrorIs(t, New().Start(context.Background(), rt), environment.ErrInvalidEnvironment)
	assert.Nil(t, New().Service())
}

func TestStartBuildsServiceFromEnvironment(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{JWTSecret: secret, AccessTokenTTL: 5 * time.Minute, JWTAudience: "billing-api"})
	var configured iamconfig.DefaultConfig
	c := New(Config{
		User:      directory{},
		Tenant:    directory{},
		Configure: func(dc *iamconfig.DefaultConfig) { configured = *dc },
	})
	require.NoError(t, c.Start(context.Background(), rt))
	require.NotNil(t, c.Service())
	assert.Equal(t, secret, configured.JWTSecret)
	assert.Equal(t, 5*time.Minute, configured.Security.AccessTokenTTL)
	assert.Equal(t, "billing-api", configured.Security.Audience)
	assert.True(t, configured.Security.VerifyIssuer, "ApplyDefaults verifies the issuer")

	ctx := context.Background()
	res, err := c.Service().Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "x"})
	require.NoError(t, err)
	s, err := c.Service().SelectTenant(ctx, port.SelectTenantInput{UserID: res.UserID, TenantID: "t1", Ticket: res.Ticket})
	require.NoError(t, err)
	claims, err := c.Service().ValidateToken(ctx, s.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "u1", claims.UserID)
}

// The component inherits the secure default: SelectTenant needs the ticket from Authenticate.
func TestStartRequiresTenantTicket(t *testing.T) {
	c := New(Config{User: directory{}, Tenant: directory{}})
	require.NoError(t, c.Start(context.Background(), gofi.NewRuntime(&environment.Environment{JWTSecret: secret})))
	_, err := c.Service().SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1"})
	assert.ErrorIs(t, err, core.ErrInvalidTenantTicket)
}

func TestStartWithoutPortsServesTokenValidationOnly(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{JWTSecret: secret})
	c := New()
	require.NoError(t, c.Start(context.Background(), rt))
	_, err := c.Service().Authenticate(context.Background(), port.AuthInput{})
	assert.ErrorIs(t, err, core.ErrLoginPortsRequired)
}

func TestFromServiceUsesCallerService(t *testing.T) {
	svc, err := iamsdk.New(iamsdk.Config{Session: memory.NewTestProvider()})
	require.NoError(t, err)
	c := FromService(svc)
	require.NoError(t, c.Start(context.Background(), gofi.NewRuntime(&environment.Environment{})))
	assert.Same(t, svc, c.Service())
}

func TestInsecureTransports(t *testing.T) {
	logging.NewLogger("test")
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "prod", CacheType: "redis", CacheURI: "10.0.0.5:6379"}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, "cache", found[0].Resource)
	assert.ErrorIs(t, configcore.CheckTransport(env, found), configcore.ErrInsecureTransport, "refused in prod")
	env.AllowInsecureTransport = "cache"
	assert.NoError(t, configcore.CheckTransport(env, found), "allowed by the hatch")

	assert.Empty(t, FromService(nil).InsecureTransports(env), "injected services are the caller's")
	env.CacheType = ""
	assert.Empty(t, New().InsecureTransports(env), "in-memory sessions")
}

// The component used to fall back to in-memory sessions silently.
func TestStartRefusesInMemoryStateInProd(t *testing.T) {
	for _, env := range []string{"prod", "stage"} {
		rt := gofi.NewRuntime(&environment.Environment{AppEnvironment: env, JWTSecret: secret})
		assert.ErrorIs(t, New().Start(context.Background(), rt), config.ErrIAMInMemorySessions, env)
	}
}

// With CACHE_TYPE=redis, sessions, throttling and tickets are shared in Redis.
func TestStartWiresRedisThrottlingAndTickets(t *testing.T) {
	mr := miniredis.RunT(t)
	rt := gofi.NewRuntime(&environment.Environment{
		AppEnvironment: "prod", JWTSecret: secret, CacheType: "redis", CacheURI: mr.Addr(),
		IAMLoginMaxAttempts: 1, IAMLoginLockout: time.Minute,
	})
	c := New(Config{User: directory{}, Tenant: directory{}})
	require.NoError(t, c.Start(context.Background(), rt))
	ctx := context.Background()

	res, err := c.Service().Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "x"})
	require.NoError(t, err)
	in := port.SelectTenantInput{UserID: res.UserID, TenantID: "t1", Ticket: res.Ticket}
	_, err = c.Service().SelectTenant(ctx, in)
	require.NoError(t, err)
	_, err = c.Service().SelectTenant(ctx, in)
	assert.ErrorIs(t, err, core.ErrInvalidTenantTicket, "single-use ticket in Redis")

	c = New(Config{User: wrongPassword{}, Tenant: directory{}})
	require.NoError(t, c.Start(ctx, rt))
	_, err = c.Service().Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "guess"})
	assert.ErrorIs(t, err, core.ErrInvalidCredentials)
	_, err = c.Service().Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "guess"})
	assert.ErrorIs(t, err, core.ErrTooManyAttempts, "IAM_LOGIN_MAX_ATTEMPTS=1")
	assert.True(t, slices.ContainsFunc(mr.Keys(), func(k string) bool { return strings.HasPrefix(k, "iam:throttle:") }))
}

// wrongPassword rejects every password.
type wrongPassword struct{ directory }

func (wrongPassword) ValidatePassword(context.Context, string, string) error {
	return core.ErrInvalidCredentials
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "iam", New().Name())
	assert.Equal(t, gofi.StageIAM, New().Stage())
}

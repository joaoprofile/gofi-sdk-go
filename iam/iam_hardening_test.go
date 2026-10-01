package iam

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/bcrypt"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/memory"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/password"
	redisprovider "github.com/gofi-labs/gofi-sdk-go/iam/provider/redis"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ClockSkewCapped(t *testing.T) {
	cfg := Config{Session: memory.NewTestProvider(), Security: SecurityConfig{ClockSkew: 6 * time.Minute}}
	assert.ErrorIs(t, validate(cfg), core.ErrClockSkewExceeded)
	cfg.Security.ClockSkew = 5 * time.Minute
	assert.NoError(t, validate(cfg))
}

func TestValidate_DummyPasswordHash(t *testing.T) {
	cfg := Config{Session: memory.NewTestProvider(), Security: SecurityConfig{DummyPasswordHash: "plain"}}
	assert.Error(t, validate(cfg))
	h, err := password.Hash("x")
	require.NoError(t, err)
	cfg.Security.DummyPasswordHash = h
	assert.NoError(t, validate(cfg))
}

func loginDir(t *testing.T) *directoryStub {
	t.Helper()
	hash, err := bcrypt.Hash("secret", bcrypt.MinCost)
	require.NoError(t, err)
	return &directoryStub{user: &types.User{ID: "u1", Email: "a@b.c", PasswordHash: hash, Active: true}}
}

// assertHardenedLogin checks throttling and single-use tickets end to end.
func assertHardenedLogin(t *testing.T, cfg DefaultConfig) {
	t.Helper()
	dir := loginDir(t)
	cfg.JWTSecret, cfg.User, cfg.Tenant, cfg.RBAC = "a-32-byte-secret-key-for-testing!", dir, dir, &stubRBACPort{}
	cfg.LoginMaxAttempts = 2
	svc, err := NewDefault(cfg)
	require.NoError(t, err)
	ctx := context.Background()

	res, err := svc.Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "secret"})
	require.NoError(t, err)
	in := port.SelectTenantInput{UserID: res.UserID, TenantID: "t1", Module: "app", Ticket: res.Ticket}
	_, err = svc.SelectTenant(ctx, in)
	require.NoError(t, err)
	_, err = svc.SelectTenant(ctx, in)
	assert.ErrorIs(t, err, core.ErrInvalidTenantTicket, "ticket is single-use")

	for range 2 {
		_, err = svc.Authenticate(ctx, port.AuthInput{Email: "A@B.C", Password: "wrong", IPAddress: "10.0.0.1"})
		assert.ErrorIs(t, err, core.ErrInvalidCredentials)
	}
	_, err = svc.Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "secret"})
	assert.ErrorIs(t, err, core.ErrTooManyAttempts, "locked out even with the right password")
}

func TestNewDefault_MemoryThrottlesAndConsumesTickets(t *testing.T) {
	assertHardenedLogin(t, DefaultConfig{})
}

func TestNewDefault_RedisThrottlesAndConsumesTickets(t *testing.T) {
	mr := miniredis.RunT(t)
	assertHardenedLogin(t, DefaultConfig{RedisAddr: mr.Addr()})
	assert.NotEmpty(t, mr.Keys())
}

func TestDefaultStores(t *testing.T) {
	s := defaultStores(DefaultConfig{LoginMaxAttempts: -1})
	assert.Nil(t, s.throttler, "negative disables throttling")
	assert.IsType(t, &memory.TicketStore{}, s.tickets)

	mr := miniredis.RunT(t)
	s = defaultStores(DefaultConfig{RedisAddr: mr.Addr()})
	assert.IsType(t, &redisprovider.LoginThrottler{}, s.throttler)
	assert.IsType(t, &redisprovider.TicketStore{}, s.tickets)
	s = defaultStores(DefaultConfig{RedisAddr: mr.Addr(), LoginMaxAttempts: -1})
	assert.Nil(t, s.throttler)
}

package iam

import (
	"context"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/bcrypt"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directoryStub is a single-user, single-tenant UserPort and TenantPort.
type directoryStub struct{ user *types.User }

func (d *directoryStub) FindByID(context.Context, string) (*types.User, error) { return d.user, nil }
func (d *directoryStub) FindByEmail(_ context.Context, email string) (*types.User, error) {
	if email != d.user.Email {
		return nil, core.ErrInvalidCredentials
	}
	return d.user, nil
}
func (d *directoryStub) ValidatePassword(_ context.Context, _, password string) error {
	return bcrypt.Compare(d.user.PasswordHash, password)
}
func (d *directoryStub) FindOrCreateByExternalIdentity(context.Context, types.ExternalIdentity) (*types.User, error) {
	return d.user, nil
}
func (d *directoryStub) ListUserTenants(context.Context, string) ([]types.TenantAccess, error) {
	return []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}, Modules: []string{"app"}, Roles: []string{"admin"}}}, nil
}
func (d *directoryStub) AssertAccess(context.Context, string, string, string) error { return nil }

func TestNewDefault_WithPorts_RunsTheLoginFlow(t *testing.T) {
	hash, err := bcrypt.Hash("secret", bcrypt.MinCost)
	require.NoError(t, err)
	dir := &directoryStub{user: &types.User{ID: "u1", Email: "a@b.c", PasswordHash: hash, Active: true}}

	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		User:      dir,
		Tenant:    dir,
		RBAC:      &stubRBACPort{},
	})
	require.NoError(t, err)
	ctx := context.Background()

	res, err := svc.Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "secret"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Ticket, "NewDefault derives the ticket secret from JWTSecret")

	s, err := svc.SelectTenant(ctx, port.SelectTenantInput{UserID: res.UserID, TenantID: "t1", Module: "app", Ticket: res.Ticket})
	require.NoError(t, err)

	claims, err := svc.ValidateToken(ctx, s.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, []string{"admin"}, claims.Roles)
	assert.True(t, svc.RBAC().Enforce(*claims, "any", "any"))

	_, err = svc.RefreshToken(ctx, s.RefreshToken)
	require.NoError(t, err)
}

func TestNewDefault_WithoutPorts_LoginFailsInsteadOfPanicking(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{JWTSecret: "a-32-byte-secret-key-for-testing!"})
	require.NoError(t, err)
	ctx := context.Background()

	_, err = svc.Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "x"})
	assert.ErrorIs(t, err, core.ErrLoginPortsRequired)
	_, err = svc.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1"})
	assert.ErrorIs(t, err, core.ErrLoginPortsRequired)
	_, err = svc.RefreshToken(ctx, "any")
	assert.ErrorIs(t, err, core.ErrLoginPortsRequired)
	_, err = svc.ListTenants(ctx, "u1")
	assert.ErrorIs(t, err, core.ErrLoginPortsRequired)
}

// Attack: POST /select-tenant {"user_id": "<victim>"} without the ticket from Authenticate.
func TestNewDefault_SelectTenantWithoutTicketIsRejected(t *testing.T) {
	dir := &directoryStub{user: &types.User{ID: "victim", Active: true}}
	svc, err := NewDefault(DefaultConfig{JWTSecret: "a-32-byte-secret-key-for-testing!", User: dir, Tenant: dir})
	require.NoError(t, err)

	for _, tk := range []string{"", "forged.ticket"} {
		_, err = svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "victim", TenantID: "t1", Module: "app", Ticket: tk})
		assert.ErrorIs(t, err, core.ErrInvalidTenantTicket)
	}
}

// A deactivated user cannot keep a session alive through refresh.
func TestNewDefault_RefreshRejectsDeactivatedUser(t *testing.T) {
	hash, err := bcrypt.Hash("secret", bcrypt.MinCost)
	require.NoError(t, err)
	dir := &directoryStub{user: &types.User{ID: "u1", Email: "a@b.c", PasswordHash: hash, Active: true}}
	svc, err := NewDefault(DefaultConfig{JWTSecret: "a-32-byte-secret-key-for-testing!", User: dir, Tenant: dir})
	require.NoError(t, err)
	ctx := context.Background()

	res, err := svc.Authenticate(ctx, port.AuthInput{Email: "a@b.c", Password: "secret"})
	require.NoError(t, err)
	s, err := svc.SelectTenant(ctx, port.SelectTenantInput{UserID: res.UserID, TenantID: "t1", Module: "app", Ticket: res.Ticket})
	require.NoError(t, err)

	dir.user = &types.User{ID: "u1", Active: false}
	_, err = svc.RefreshToken(ctx, s.RefreshToken)
	assert.ErrorIs(t, err, core.ErrAccountInactive)
	_, err = svc.ValidateToken(ctx, s.AccessToken)
	assert.ErrorIs(t, err, core.ErrSessionRevoked, "the session is revoked, not just the refresh")
}

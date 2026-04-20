package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubUserPort is a test double for port.UserPort.
type stubUserPort struct {
	user         *types.User
	findErr      error
	validateErr  error
	externalUser *types.User
	externalErr  error
}

func (s *stubUserPort) FindByID(_ context.Context, _ string) (*types.User, error) {
	return s.user, s.findErr
}
func (s *stubUserPort) FindByEmail(_ context.Context, _ string) (*types.User, error) {
	return s.user, s.findErr
}
func (s *stubUserPort) ValidatePassword(_ context.Context, _, _ string) error {
	return s.validateErr
}
func (s *stubUserPort) FindOrCreateByExternalIdentity(_ context.Context, _ types.ExternalIdentity) (*types.User, error) {
	return s.externalUser, s.externalErr
}

// stubTenantPort is a test double for port.TenantPort.
type stubTenantPort struct {
	tenants    []types.TenantAccess
	tenantsErr error
	accessErr  error
}

func (s *stubTenantPort) ListUserTenants(_ context.Context, _ string) ([]types.TenantAccess, error) {
	return s.tenants, s.tenantsErr
}
func (s *stubTenantPort) AssertAccess(_ context.Context, _, _, _ string) error {
	return s.accessErr
}

// stubTokenPort is a test double for port.TokenPort.
type stubTokenPort struct {
	accessToken string
	tokenErr    error
	claims      *types.Claims
	parseErr    error
}

func (s *stubTokenPort) IssueAccessToken(_ types.Claims) (string, error) {
	return s.accessToken, s.tokenErr
}
func (s *stubTokenPort) IssueRefreshToken(_ types.Claims) (string, error) {
	return "refresh-token", nil
}
func (s *stubTokenPort) ParseToken(_ string) (*types.Claims, error) {
	return s.claims, s.parseErr
}

// buildLocalAuth creates a localAuth with an in-memory session and configurable stubs.
func buildLocalAuth(
	user *stubUserPort,
	tenant *stubTenantPort,
	token *stubTokenPort,
) (port.AuthPort, *memSession) {
	sess := newMemSession()
	cfg := AuthConfig{
		accessTokenTTL:  15 * time.Minute,
		refreshTokenTTL: 7 * 24 * time.Hour,
		issuer:          "test-issuer",
	}
	auth := NewLocalAuth(LocalAuthConfig{
		User:    user,
		Tenant:  tenant,
		Token:   token,
		Session: sess,
		Cfg:     cfg,
	})
	return auth, sess
}

func TestLocalAuth_Authenticate_Success(t *testing.T) {
	user := &stubUserPort{
		user: &types.User{ID: "u1", Email: "a@b.com", Active: true},
	}
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{
			{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"admin"}},
		},
	}
	token := &stubTokenPort{accessToken: "access"}
	auth, _ := buildLocalAuth(user, tenant, token)

	result, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "pass"})
	require.NoError(t, err)
	assert.Equal(t, "u1", result.UserID)
	assert.Len(t, result.Tenants, 1)
}

func TestLocalAuth_Authenticate_UserNotFound(t *testing.T) {
	user := &stubUserPort{findErr: errors.New("not found")}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "x@x.com", Password: "p"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLocalAuth_Authenticate_InactiveUser(t *testing.T) {
	user := &stubUserPort{
		user: &types.User{ID: "u1", Active: false},
	}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "p"})
	assert.ErrorIs(t, err, ErrAccountInactive)
}

func TestLocalAuth_Authenticate_WrongPassword(t *testing.T) {
	user := &stubUserPort{
		user:        &types.User{ID: "u1", Active: true},
		validateErr: errors.New("wrong password"),
	}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "wrong"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLocalAuth_SelectTenant_Success(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{
			{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"viewer"}},
		},
	}
	token := &stubTokenPort{accessToken: "access-token"}
	auth, sess := buildLocalAuth(user, tenant, token)

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID:   "u1",
		TenantID: "t1",
		Module:   "mod",
	})
	require.NoError(t, err)
	assert.Equal(t, "u1", session.UserID)
	assert.Equal(t, "t1", session.TenantID)
	assert.Equal(t, "access-token", session.AccessToken)
	assert.NotEmpty(t, session.RefreshToken)
	assert.NotEmpty(t, session.RefreshTokenHash)

	// Session must be persisted.
	stored, err := sess.Get(context.Background(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, stored.ID)
	// Raw refresh token must not be stored.
	assert.Empty(t, stored.RefreshToken)
}

func TestLocalAuth_SelectTenant_AccessDenied(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{accessErr: errors.New("denied")}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID:   "u1",
		TenantID: "t1",
	})
	assert.ErrorIs(t, err, ErrTenantAccessDenied)
}

func TestLocalAuth_SelectTenant_TokenIssueError(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}},
	}
	token := &stubTokenPort{tokenErr: errors.New("signing failed")}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID:   "u1",
		TenantID: "t1",
	})
	assert.Error(t, err)
}

func TestLocalAuth_Logout_Success(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	auth, sess := buildLocalAuth(user, tenant, token)

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	err = auth.Logout(context.Background(), session.ID)
	require.NoError(t, err)

	stored, err := sess.Get(context.Background(), session.ID)
	require.NoError(t, err)
	assert.True(t, stored.Revoked)
}

func TestLocalAuth_Logout_SessionNotFound(t *testing.T) {
	user := &stubUserPort{}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	err := auth.Logout(context.Background(), "nonexistent-session")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestLocalAuth_RefreshToken_Success(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "new-access-token"}
	auth, _ := buildLocalAuth(user, tenant, token)

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	newSession, err := auth.RefreshToken(context.Background(), session.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, session.ID, newSession.ID)
	assert.Equal(t, "u1", newSession.UserID)
	assert.NotEmpty(t, newSession.RefreshToken)
	assert.NotEqual(t, session.RefreshToken, newSession.RefreshToken)
}

func TestLocalAuth_RefreshToken_RevokedSession(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	auth, _ := buildLocalAuth(user, tenant, token)

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	err = auth.Logout(context.Background(), session.ID)
	require.NoError(t, err)

	// Reusing a revoked session's refresh token must fail.
	_, err = auth.RefreshToken(context.Background(), session.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionRevoked)
}

func TestLocalAuth_RefreshToken_ExpiredSession(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	sess := newMemSession()
	cfg := AuthConfig{
		accessTokenTTL:  15 * time.Minute,
		refreshTokenTTL: -1 * time.Second, // already expired
		issuer:          "test",
	}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: token, Session: sess, Cfg: cfg,
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	_, err = auth.RefreshToken(context.Background(), session.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestLocalAuth_RefreshToken_InvalidFormat(t *testing.T) {
	user := &stubUserPort{}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.RefreshToken(context.Background(), "no-dot-separator")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestLocalAuth_RefreshToken_HashMismatch(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	auth, _ := buildLocalAuth(user, tenant, token)

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	// Tamper with the token but keep the session ID prefix valid.
	tampered := session.ID + ".tampered-random-suffix"
	_, err = auth.RefreshToken(context.Background(), tampered)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestLocalAuth_ValidateToken_Success(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	sess := newMemSession()
	cfg := AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: 7 * 24 * time.Hour, issuer: "test"}

	tokenPort := &captureSessionIDTokenPort{accessToken: "my-access-token"}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: tokenPort, Session: sess, Cfg: cfg,
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	tokenPort.parseClaims = &types.Claims{
		UserID:    "u1",
		TenantID:  "t1",
		SessionID: session.ID,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}

	claims, err := auth.ValidateToken(context.Background(), "my-access-token")
	require.NoError(t, err)
	assert.Equal(t, "u1", claims.UserID)
}

// captureSessionIDTokenPort is a stub that supports returning configurable claims on parse.
type captureSessionIDTokenPort struct {
	accessToken string
	parseClaims *types.Claims
}

func (c *captureSessionIDTokenPort) IssueAccessToken(_ types.Claims) (string, error) {
	return c.accessToken, nil
}
func (c *captureSessionIDTokenPort) IssueRefreshToken(_ types.Claims) (string, error) {
	return "refresh", nil
}
func (c *captureSessionIDTokenPort) ParseToken(_ string) (*types.Claims, error) {
	if c.parseClaims == nil {
		return nil, ErrTokenInvalid
	}
	return c.parseClaims, nil
}

func TestLocalAuth_ValidateToken_ParseError(t *testing.T) {
	user := &stubUserPort{}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{parseErr: ErrTokenInvalid}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.ValidateToken(context.Background(), "bad-token")
	assert.Error(t, err)
}

func TestLocalAuth_ValidateToken_SessionNotFound(t *testing.T) {
	user := &stubUserPort{}
	tenant := &stubTenantPort{}
	token := &stubTokenPort{
		claims: &types.Claims{
			UserID:    "u1",
			SessionID: "nonexistent-session",
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	auth, _ := buildLocalAuth(user, tenant, token)

	_, err := auth.ValidateToken(context.Background(), "any-token")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestLocalAuth_ValidateToken_RevokedSession(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	sess := newMemSession()
	cfg := AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: 7 * 24 * time.Hour, issuer: "test"}
	tokenPort := &captureSessionIDTokenPort{accessToken: "tok"}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: tokenPort, Session: sess, Cfg: cfg,
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	err = auth.Logout(context.Background(), session.ID)
	require.NoError(t, err)

	tokenPort.parseClaims = &types.Claims{
		UserID:    "u1",
		SessionID: session.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	_, err = auth.ValidateToken(context.Background(), "tok")
	assert.ErrorIs(t, err, ErrSessionRevoked)
}

func TestLocalAuth_ValidateToken_ExpiredSession(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	sess := newMemSession()
	cfg := AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: -time.Second, issuer: "test"}
	tokenPort := &captureSessionIDTokenPort{accessToken: "tok"}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: tokenPort, Session: sess, Cfg: cfg,
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	tokenPort.parseClaims = &types.Claims{
		UserID:    "u1",
		SessionID: session.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	_, err = auth.ValidateToken(context.Background(), "tok")
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestRolesForTenant_Found(t *testing.T) {
	tenants := []types.TenantAccess{
		{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"admin", "viewer"}},
		{Tenant: types.Tenant{ID: "t2"}, Roles: []string{"editor"}},
	}
	roles := rolesForTenant(tenants, "t1")
	assert.Equal(t, []string{"admin", "viewer"}, roles)
}

func TestRolesForTenant_NotFound(t *testing.T) {
	tenants := []types.TenantAccess{
		{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"admin"}},
	}
	roles := rolesForTenant(tenants, "nonexistent")
	assert.Nil(t, roles)
}

func TestRolesForTenant_EmptyList(t *testing.T) {
	roles := rolesForTenant(nil, "t1")
	assert.Nil(t, roles)
}

func TestLocalAuth_EmitNilFallback(t *testing.T) {
	auth := NewLocalAuth(LocalAuthConfig{
		User:    &stubUserPort{findErr: errors.New("not found")},
		Tenant:  &stubTenantPort{},
		Token:   &stubTokenPort{},
		Session: newMemSession(),
		Cfg:     AuthConfig{},
		Emit:    nil,
	})
	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "x", Password: "y"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

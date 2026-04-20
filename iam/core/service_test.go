package core

import (
	"context"
	"errors"
	"testing"
	"time"

	iamconfig "github.com/joaoprofile/gofi/iam/config"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRBACPort is a test double for port.RBACPort.
type stubRBACPort struct {
	enforceResult bool
	permissions   []port.Permission
}

func (s *stubRBACPort) Enforce(_ types.Claims, _, _ string) bool { return s.enforceResult }
func (s *stubRBACPort) Permissions(_ types.Claims) []port.Permission {
	return s.permissions
}

// stubAuthPort is a test double for port.AuthPort.
type stubAuthPort struct {
	authResult  *port.AuthResult
	authErr     error
	session     *types.Session
	selectErr   error
	logoutErr   error
	refreshSess *types.Session
	refreshErr  error
	claims      *types.Claims
	validateErr error
}

func (s *stubAuthPort) Authenticate(_ context.Context, _ port.AuthInput) (*port.AuthResult, error) {
	return s.authResult, s.authErr
}
func (s *stubAuthPort) SelectTenant(_ context.Context, _ port.SelectTenantInput) (*types.Session, error) {
	return s.session, s.selectErr
}
func (s *stubAuthPort) Logout(_ context.Context, _ string) error { return s.logoutErr }
func (s *stubAuthPort) RefreshToken(_ context.Context, _ string) (*types.Session, error) {
	return s.refreshSess, s.refreshErr
}
func (s *stubAuthPort) ValidateToken(_ context.Context, _ string) (*types.Claims, error) {
	return s.claims, s.validateErr
}

func buildTestService(auth port.AuthPort, tenant port.TenantPort, rbac port.RBACPort, sess port.SessionPort) *IAMService {
	return NewService(ServiceConfig{
		Auth:    auth,
		Tenant:  tenant,
		RBAC:    rbac,
		Session: sess,
		IDPs:    make(map[string]*IDPService),
	})
}

func TestNewService_NilOnEventFallback(t *testing.T) {
	svc := NewService(ServiceConfig{
		IDPs: make(map[string]*IDPService),
	})
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.emit)
}

func TestIAMService_Authenticate_Delegates(t *testing.T) {
	auth := &stubAuthPort{
		authResult: &port.AuthResult{UserID: "u1"},
	}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	result, err := svc.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "p"})
	require.NoError(t, err)
	assert.Equal(t, "u1", result.UserID)
}

func TestIAMService_Authenticate_PropagatesError(t *testing.T) {
	auth := &stubAuthPort{authErr: ErrInvalidCredentials}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	_, err := svc.Authenticate(context.Background(), port.AuthInput{})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestIAMService_SelectTenant_Delegates(t *testing.T) {
	expected := &types.Session{ID: "sess1", UserID: "u1"}
	auth := &stubAuthPort{session: expected}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	sess, err := svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1"})
	require.NoError(t, err)
	assert.Equal(t, "sess1", sess.ID)
}

func TestIAMService_Logout_Delegates(t *testing.T) {
	auth := &stubAuthPort{}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	err := svc.Logout(context.Background(), "session-1")
	assert.NoError(t, err)
}

func TestIAMService_Logout_PropagatesError(t *testing.T) {
	auth := &stubAuthPort{logoutErr: errors.New("cannot logout")}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	err := svc.Logout(context.Background(), "session-1")
	assert.Error(t, err)
}

func TestIAMService_LogoutAll_RevokesAllSessions(t *testing.T) {
	sess := newMemSession()
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, sess)

	now := time.Now()
	_ = sess.Save(context.Background(), &types.Session{
		ID: "s1", UserID: "u1", ExpiresAt: now.Add(time.Hour),
	})
	_ = sess.Save(context.Background(), &types.Session{
		ID: "s2", UserID: "u1", ExpiresAt: now.Add(time.Hour),
	})

	err := svc.LogoutAll(context.Background(), "u1")
	require.NoError(t, err)

	s1, _ := sess.Get(context.Background(), "s1")
	s2, _ := sess.Get(context.Background(), "s2")
	assert.True(t, s1.Revoked)
	assert.True(t, s2.Revoked)
}

func TestIAMService_RefreshToken_Delegates(t *testing.T) {
	expected := &types.Session{ID: "new-session"}
	auth := &stubAuthPort{refreshSess: expected}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	sess, err := svc.RefreshToken(context.Background(), "refresh-tok")
	require.NoError(t, err)
	assert.Equal(t, "new-session", sess.ID)
}

func TestIAMService_ValidateToken_Delegates(t *testing.T) {
	expected := &types.Claims{UserID: "u1"}
	auth := &stubAuthPort{claims: expected}
	svc := buildTestService(auth, &stubTenantPort{}, &stubRBACPort{}, newMemSession())

	claims, err := svc.ValidateToken(context.Background(), "tok")
	require.NoError(t, err)
	assert.Equal(t, "u1", claims.UserID)
}

func TestIAMService_ListSessions_ReturnsActiveSessions(t *testing.T) {
	sess := newMemSession()
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, sess)

	now := time.Now()
	_ = sess.Save(context.Background(), &types.Session{
		ID: "s1", UserID: "u1", ExpiresAt: now.Add(time.Hour),
	})

	sessions, err := svc.ListSessions(context.Background(), "u1")
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

func TestIAMService_ListTenants_Delegates(t *testing.T) {
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{
			{Tenant: types.Tenant{ID: "t1"}},
		},
	}
	svc := buildTestService(&stubAuthPort{}, tenant, &stubRBACPort{}, newMemSession())

	tenants, err := svc.ListTenants(context.Background(), "u1")
	require.NoError(t, err)
	assert.Len(t, tenants, 1)
}

func TestIAMService_RBAC_ReturnsPort(t *testing.T) {
	rbac := &stubRBACPort{enforceResult: true}
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, rbac, newMemSession())
	assert.Equal(t, rbac, svc.RBAC())
}

func TestIAMService_Tenant_ReturnsPort(t *testing.T) {
	tenant := &stubTenantPort{}
	svc := buildTestService(&stubAuthPort{}, tenant, &stubRBACPort{}, newMemSession())
	assert.Equal(t, tenant, svc.Tenant())
}

func TestIAMService_Session_ReturnsPort(t *testing.T) {
	sess := newMemSession()
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, sess)
	assert.Equal(t, sess, svc.Session())
}

func TestIAMService_IDPInitFlow_ProviderNotFound(t *testing.T) {
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, newMemSession())
	_, err := svc.IDPInitFlow(context.Background(), "nonexistent", "", nil)
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

func TestIAMService_IDPHandleCallback_ProviderNotFound(t *testing.T) {
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, newMemSession())
	_, err := svc.IDPHandleCallback(context.Background(), "nonexistent", port.IDPCallbackInput{})
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

func TestIAMService_IDPSelectTenant_ProviderNotFound(t *testing.T) {
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, newMemSession())
	_, err := svc.IDPSelectTenant(context.Background(), "nonexistent", port.SelectTenantInput{})
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

func TestAuthConfigFromSecurity_MapsFields(t *testing.T) {
	sec := iamconfig.SecurityConfig{
		AccessTokenTTL:  10 * time.Minute,
		RefreshTokenTTL: 3 * 24 * time.Hour,
		Issuer:          "my-issuer",
	}
	cfg := AuthConfigFromSecurity(sec)
	assert.Equal(t, 10*time.Minute, cfg.accessTokenTTL)
	assert.Equal(t, 3*24*time.Hour, cfg.refreshTokenTTL)
	assert.Equal(t, "my-issuer", cfg.issuer)
}

// --- helpers for IDP-registered service ---

type stubSessionPort struct {
	sessions     map[string]*types.Session
	saveErr      error
	revokeErr    error
	revokeAllErr error
}

func newStubSessionPort() *stubSessionPort {
	return &stubSessionPort{sessions: make(map[string]*types.Session)}
}

func (s *stubSessionPort) Save(_ context.Context, sess *types.Session) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.sessions[sess.ID] = sess
	return nil
}
func (s *stubSessionPort) Get(_ context.Context, id string) (*types.Session, error) {
	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return sess, nil
}
func (s *stubSessionPort) Revoke(_ context.Context, id string) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	if sess, ok := s.sessions[id]; ok {
		sess.Revoked = true
	}
	return nil
}
func (s *stubSessionPort) RevokeAllForUser(_ context.Context, _ string) error {
	return s.revokeAllErr
}
func (s *stubSessionPort) ListByUser(_ context.Context, userID string) ([]*types.Session, error) {
	var out []*types.Session
	for _, s := range s.sessions {
		if s.UserID == userID && !s.Revoked {
			out = append(out, s)
		}
	}
	return out, nil
}

func buildServiceWithIDP(idp *IDPService) *IAMService {
	return NewService(ServiceConfig{
		Auth:    &stubAuthPort{},
		Tenant:  &stubTenantPort{},
		RBAC:    &stubRBACPort{},
		Session: newStubSessionPort(),
		IDPs:    map[string]*IDPService{"test-idp": idp},
	})
}

// --- LogoutAll error path ---

func TestIAMService_LogoutAll_Error(t *testing.T) {
	sess := newStubSessionPort()
	sess.revokeAllErr = errors.New("redis unavailable")
	svc := buildTestService(&stubAuthPort{}, &stubTenantPort{}, &stubRBACPort{}, sess)

	err := svc.LogoutAll(context.Background(), "u1")
	assert.ErrorIs(t, err, sess.revokeAllErr)
}

// --- IDPInitFlow / IDPHandleCallback / IDPSelectTenant with registered provider ---

func TestIAMService_IDPInitFlow_Success(t *testing.T) {
	expectedURL := &port.IDPAuthURL{URL: "https://idp.example.com/auth"}
	idpSvc := buildIDPService(
		&stubIDPAuthPort{name: "test-idp", authURL: expectedURL},
		&stubUserPort{}, &stubTenantPort{}, &stubTokenPort{},
	)
	svc := buildServiceWithIDP(idpSvc)

	result, err := svc.IDPInitFlow(context.Background(), "test-idp", "https://app/callback", nil)
	require.NoError(t, err)
	assert.Equal(t, expectedURL.URL, result.URL)
}

func TestIAMService_IDPHandleCallback_Success(t *testing.T) {
	cbResult := &port.IDPCallbackResult{
		IDPUser:   types.IDPUser{ExternalID: "ext-1", Provider: "test-idp"},
		IsNewUser: false,
	}
	idpSvc := buildIDPService(
		&stubIDPAuthPort{name: "test-idp", callbackResult: cbResult},
		&stubUserPort{externalUser: &types.User{ID: "u1", Active: true}},
		&stubTenantPort{},
		&stubTokenPort{},
	)
	svc := buildServiceWithIDP(idpSvc)

	result, err := svc.IDPHandleCallback(context.Background(), "test-idp", port.IDPCallbackInput{})
	require.NoError(t, err)
	assert.False(t, result.IsNewUser)
}

func TestIAMService_IDPSelectTenant_Success(t *testing.T) {
	idpSvc := buildIDPService(
		&stubIDPAuthPort{name: "test-idp"},
		&stubUserPort{user: &types.User{ID: "u1", Active: true}},
		&stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}},
		&stubTokenPort{accessToken: "tok"},
	)
	svc := buildServiceWithIDP(idpSvc)

	sess, err := svc.IDPSelectTenant(context.Background(), "test-idp", port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)
	assert.Equal(t, "u1", sess.UserID)
}

// --- LocalAuth missing error paths ---

func TestLocalAuth_Authenticate_ListTenantsError(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Email: "a@b.com", Active: true}}
	tenant := &stubTenantPort{tenantsErr: errors.New("db error")}
	auth, _ := buildLocalAuth(user, tenant, &stubTokenPort{})

	_, err := auth.Authenticate(context.Background(), port.AuthInput{Email: "a@b.com", Password: "pass"})
	assert.Error(t, err)
}

func TestLocalAuth_SelectTenant_ListTenantsError(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenantsErr: errors.New("db error")}
	auth, _ := buildLocalAuth(user, tenant, &stubTokenPort{})

	_, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	assert.Error(t, err)
}

func TestLocalAuth_SelectTenant_SessionSaveError(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	sess := newStubSessionPort()
	sess.saveErr = errors.New("save failed")

	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: token, Session: sess,
		Cfg: AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: 7 * 24 * time.Hour},
	})

	_, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	assert.Error(t, err)
}

func TestLocalAuth_RefreshToken_SessionSaveError(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}

	// Use in-memory session for the initial select, then switch to failing session for refresh.
	sess := newMemSession()
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: token, Session: sess,
		Cfg: AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: 7 * 24 * time.Hour},
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	// Replace the session port with one that fails on Save.
	failSess := newStubSessionPort()
	// Copy the existing session so Get works.
	failSess.sessions[session.ID] = &types.Session{
		ID:               session.ID,
		UserID:           "u1",
		RefreshTokenHash: session.RefreshTokenHash,
		ExpiresAt:        time.Now().Add(time.Hour),
		Revoked:          false,
	}
	failSess.saveErr = errors.New("save failed")

	authWithFailSess := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: token, Session: failSess,
		Cfg: AuthConfig{accessTokenTTL: 15 * time.Minute, refreshTokenTTL: 7 * 24 * time.Hour},
	})

	_, err = authWithFailSess.RefreshToken(context.Background(), session.RefreshToken)
	assert.Error(t, err)
}

func TestAuthConfigFromSecurity_SessionExpiryApplied(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{accessToken: "tok"}
	sess := newMemSession()

	cfg := AuthConfig{
		accessTokenTTL:  10 * time.Minute,
		refreshTokenTTL: 3 * 24 * time.Hour,
		issuer:          "my-issuer",
	}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: token, Session: sess, Cfg: cfg,
	})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	require.NoError(t, err)

	stored, err := sess.Get(context.Background(), session.ID)
	require.NoError(t, err)
	assert.True(t, stored.ExpiresAt.After(time.Now().Add(2*24*time.Hour)))
	assert.True(t, stored.ExpiresAt.Before(time.Now().Add(4*24*time.Hour)))
}

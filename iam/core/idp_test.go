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

// stubIDPAuthPort is a test double for port.IDPAuthPort.
type stubIDPAuthPort struct {
	name           string
	authURL        *port.IDPAuthURL
	authURLErr     error
	callbackResult *port.IDPCallbackResult
	callbackErr    error
}

func (s *stubIDPAuthPort) ProviderName() string { return s.name }
func (s *stubIDPAuthPort) AuthorizationURL(_ context.Context, _ port.IDPAuthInput) (*port.IDPAuthURL, error) {
	return s.authURL, s.authURLErr
}
func (s *stubIDPAuthPort) HandleCallback(_ context.Context, _ port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	return s.callbackResult, s.callbackErr
}

func buildIDPService(idp port.IDPAuthPort, user *stubUserPort, tenant *stubTenantPort, token *stubTokenPort) *IDPService {
	sess := newMemSession()
	cfg := AuthConfig{
		accessTokenTTL:  15 * time.Minute,
		refreshTokenTTL: 7 * 24 * time.Hour,
		issuer:          "test",
	}
	return NewIDPService(IDPServiceConfig{
		Provider: idp,
		User:     user,
		Tenant:   tenant,
		Token:    token,
		Session:  sess,
		Cfg:      cfg,
	})
}

func TestNewIDPService_NilEmitFallback(t *testing.T) {
	svc := NewIDPService(IDPServiceConfig{
		Provider: &stubIDPAuthPort{name: "test"},
		Emit:     nil,
	})
	assert.NotNil(t, svc)
	assert.NotNil(t, svc.emit)
}

func TestIDPService_InitFlow_Success(t *testing.T) {
	expectedURL := &port.IDPAuthURL{
		URL:          "https://provider.com/auth?state=abc",
		State:        "abc",
		CodeVerifier: "verifier",
	}
	idp := &stubIDPAuthPort{
		name:    "google",
		authURL: expectedURL,
	}
	svc := buildIDPService(idp, &stubUserPort{}, &stubTenantPort{}, &stubTokenPort{})

	result, err := svc.InitFlow(context.Background(), "https://myapp.com/callback", nil)
	require.NoError(t, err)
	assert.Equal(t, expectedURL.URL, result.URL)
}

func TestIDPService_InitFlow_ProviderError(t *testing.T) {
	idp := &stubIDPAuthPort{
		name:       "google",
		authURLErr: errors.New("provider error"),
	}
	svc := buildIDPService(idp, &stubUserPort{}, &stubTenantPort{}, &stubTokenPort{})

	_, err := svc.InitFlow(context.Background(), "", nil)
	assert.Error(t, err)
}

func TestIDPService_HandleCallback_NewUser(t *testing.T) {
	idpUser := types.IDPUser{
		ExternalID: "ext-123",
		Provider:   "google",
		Email:      "user@gmail.com",
	}
	callbackResult := &port.IDPCallbackResult{
		IDPUser:   idpUser,
		IsNewUser: true,
	}
	idp := &stubIDPAuthPort{
		name:           "google",
		callbackResult: callbackResult,
	}
	user := &stubUserPort{
		externalUser: &types.User{ID: "u-new", Email: "user@gmail.com", Active: true},
	}
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}},
	}
	token := &stubTokenPort{accessToken: "tok"}
	svc := buildIDPService(idp, user, tenant, token)

	result, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{
		State: "s", ExpectedState: "s",
	})
	require.NoError(t, err)
	assert.True(t, result.IsNewUser)
	assert.Equal(t, "user@gmail.com", result.IDPUser.Email)
	assert.Len(t, result.Tenants, 1)
}

func TestIDPService_HandleCallback_ExistingUser(t *testing.T) {
	idpUser := types.IDPUser{ExternalID: "ext-456", Provider: "google"}
	callbackResult := &port.IDPCallbackResult{IDPUser: idpUser, IsNewUser: false}
	idp := &stubIDPAuthPort{name: "google", callbackResult: callbackResult}
	user := &stubUserPort{externalUser: &types.User{ID: "u-existing", Active: true}}
	tenant := &stubTenantPort{}
	svc := buildIDPService(idp, user, tenant, &stubTokenPort{})

	result, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{})
	require.NoError(t, err)
	assert.False(t, result.IsNewUser)
}

func TestIDPService_HandleCallback_ProviderError(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google", callbackErr: errors.New("token exchange failed")}
	svc := buildIDPService(idp, &stubUserPort{}, &stubTenantPort{}, &stubTokenPort{})

	_, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{})
	assert.Error(t, err)
}

func TestIDPService_HandleCallback_UserCreateError(t *testing.T) {
	idpUser := types.IDPUser{ExternalID: "ext", Provider: "google"}
	callbackResult := &port.IDPCallbackResult{IDPUser: idpUser}
	idp := &stubIDPAuthPort{name: "google", callbackResult: callbackResult}
	user := &stubUserPort{externalErr: errors.New("db error")}
	svc := buildIDPService(idp, user, &stubTenantPort{}, &stubTokenPort{})

	_, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{})
	assert.Error(t, err)
}

func TestIDPService_SelectTenant_Success(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google"}
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{
		tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"viewer"}}},
	}
	token := &stubTokenPort{accessToken: "access-tok"}
	svc := buildIDPService(idp, user, tenant, token)

	session, err := svc.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1", Module: "mod",
	})
	require.NoError(t, err)
	assert.Equal(t, "u1", session.UserID)
	assert.Equal(t, "google", session.AuthProvider)
	assert.NotEmpty(t, session.RefreshToken)
}

func TestIDPService_SelectTenant_AccessDenied(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google"}
	tenant := &stubTenantPort{accessErr: errors.New("no access")}
	svc := buildIDPService(idp, &stubUserPort{}, tenant, &stubTokenPort{})

	_, err := svc.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	assert.ErrorIs(t, err, ErrTenantAccessDenied)
}

func TestIDPService_SelectTenant_TokenError(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google"}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	token := &stubTokenPort{tokenErr: errors.New("signing failed")}
	svc := buildIDPService(idp, &stubUserPort{}, tenant, token)

	_, err := svc.SelectTenant(context.Background(), port.SelectTenantInput{
		UserID: "u1", TenantID: "t1",
	})
	assert.Error(t, err)
}

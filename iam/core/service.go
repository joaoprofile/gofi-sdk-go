package core

import (
	"context"
	"fmt"
	"time"

	iamconfig "github.com/joaoprofile/gofi/iam/config"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
)

// IAMService is the central facade of the iam package.
// Orchestrates authentication, session management, social IDP login, and RBAC.
// Obtained via iam.New() or iam.NewDefault().
type IAMService struct {
	auth    port.AuthPort
	session port.SessionPort
	tenant  port.TenantPort
	rbac    port.RBACPort
	idps    map[string]*IDPService
	emit    func(context.Context, types.IAMEvent)
}

// ServiceConfig holds the internal parameters for building an IAMService.
type ServiceConfig struct {
	Auth    port.AuthPort
	Session port.SessionPort
	Tenant  port.TenantPort
	RBAC    port.RBACPort
	IDPs    map[string]*IDPService
	OnEvent func(context.Context, types.IAMEvent)
}

// NewService builds an IAMService from validated configuration.
// Not intended to be called directly — use iam.New() or iam.NewDefault().
func NewService(cfg ServiceConfig) *IAMService {
	emit := cfg.OnEvent
	if emit == nil {
		emit = func(context.Context, types.IAMEvent) {}
	}
	return &IAMService{
		auth:    cfg.Auth,
		session: cfg.Session,
		tenant:  cfg.Tenant,
		rbac:    cfg.RBAC,
		idps:    cfg.IDPs,
		emit:    emit,
	}
}

// AuthConfigFromSecurity converts SecurityConfig to the internal AuthConfig.
func AuthConfigFromSecurity(sec iamconfig.SecurityConfig) AuthConfig {
	return AuthConfig{
		accessTokenTTL:  sec.AccessTokenTTL,
		refreshTokenTTL: sec.RefreshTokenTTL,
		issuer:          sec.Issuer,
	}
}

// Authenticate validates credentials and returns available tenants without issuing tokens.
func (s *IAMService) Authenticate(ctx context.Context, input port.AuthInput) (*port.AuthResult, error) {
	return s.auth.Authenticate(ctx, input)
}

// SelectTenant issues tokens and creates a session for the selected tenant.
func (s *IAMService) SelectTenant(ctx context.Context, input port.SelectTenantInput) (*types.Session, error) {
	return s.auth.SelectTenant(ctx, input)
}

// Logout revokes the session — a real logout independent of token expiry.
func (s *IAMService) Logout(ctx context.Context, sessionID string) error {
	return s.auth.Logout(ctx, sessionID)
}

// LogoutAll revokes all sessions for the user across all devices.
func (s *IAMService) LogoutAll(ctx context.Context, userID string) error {
	if err := s.session.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	s.emit(ctx, types.IAMEvent{
		Type:      types.EventLogoutAll,
		UserID:    userID,
		Timestamp: time.Now(),
	})
	return nil
}

// RefreshToken renews the access token with mandatory refresh token rotation.
func (s *IAMService) RefreshToken(ctx context.Context, refreshToken string) (*types.Session, error) {
	return s.auth.RefreshToken(ctx, refreshToken)
}

// ValidateToken validates the access token and verifies the corresponding session.
func (s *IAMService) ValidateToken(ctx context.Context, accessToken string) (*types.Claims, error) {
	return s.auth.ValidateToken(ctx, accessToken)
}

// ListSessions returns the active sessions for a user for device management.
func (s *IAMService) ListSessions(ctx context.Context, userID string) ([]*types.Session, error) {
	return s.session.ListByUser(ctx, userID)
}

// ListTenants lists the available tenants for the user.
func (s *IAMService) ListTenants(ctx context.Context, userID string) ([]types.TenantAccess, error) {
	return s.tenant.ListUserTenants(ctx, userID)
}

// IDPInitFlow prepares the OAuth2/OIDC flow for the given provider.
// Returns the authorization URL and security values (state, code_verifier)
// that the caller must store in an HttpOnly cookie.
func (s *IAMService) IDPInitFlow(ctx context.Context, provider, redirectURI string, extraScopes []string) (*port.IDPAuthURL, error) {
	svc, err := s.idpFor(provider)
	if err != nil {
		return nil, err
	}
	return svc.InitFlow(ctx, redirectURI, extraScopes)
}

// IDPHandleCallback processes the IDP callback for the given provider.
func (s *IAMService) IDPHandleCallback(ctx context.Context, provider string, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	svc, err := s.idpFor(provider)
	if err != nil {
		return nil, err
	}
	return svc.HandleCallback(ctx, input)
}

// IDPSelectTenant creates a session after authentication via an IDP.
func (s *IAMService) IDPSelectTenant(ctx context.Context, provider string, input port.SelectTenantInput) (*types.Session, error) {
	svc, err := s.idpFor(provider)
	if err != nil {
		return nil, err
	}
	return svc.SelectTenant(ctx, input)
}

// RBAC returns the authorization provider for direct use in handlers.
func (s *IAMService) RBAC() port.RBACPort {
	return s.rbac
}

// Tenant returns the tenant port for use in middleware.
func (s *IAMService) Tenant() port.TenantPort {
	return s.tenant
}

// Session returns the session port.
func (s *IAMService) Session() port.SessionPort {
	return s.session
}

// idpFor retrieves the IDPService for the given provider name.
func (s *IAMService) idpFor(provider string) (*IDPService, error) {
	svc, ok := s.idps[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, provider)
	}
	return svc, nil
}

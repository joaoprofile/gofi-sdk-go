package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
)

// IDPServiceConfig holds the parameters for building an IDPService.
type IDPServiceConfig struct {
	Provider port.IDPAuthPort
	User     port.UserPort
	Tenant   port.TenantPort
	Token    port.TokenPort
	Session  port.SessionPort
	Cfg      AuthConfig
	Emit     func(context.Context, types.IAMEvent)
}

// IDPService orchestrates the social login flow (OAuth2/OIDC) for a specific provider.
type IDPService struct {
	provider port.IDPAuthPort
	user     port.UserPort
	tenant   port.TenantPort
	token    port.TokenPort
	session  port.SessionPort
	cfg      AuthConfig
	emit     func(context.Context, types.IAMEvent)
}

// NewIDPService builds an IDPService for the given provider.
func NewIDPService(cfg IDPServiceConfig) *IDPService {
	emit := cfg.Emit
	if emit == nil {
		emit = func(context.Context, types.IAMEvent) {}
	}
	return &IDPService{
		provider: cfg.Provider,
		user:     cfg.User,
		tenant:   cfg.Tenant,
		token:    cfg.Token,
		session:  cfg.Session,
		cfg:      cfg.Cfg,
		emit:     emit,
	}
}

// InitFlow prepares the OAuth2/OIDC flow by generating state, PKCE, and returning the authorization URL.
// The caller is responsible for storing IDPAuthURL.CodeVerifier and State in an HttpOnly cookie.
func (s *IDPService) InitFlow(ctx context.Context, redirectURI string, extraScopes []string) (*port.IDPAuthURL, error) {
	state, err := generateState()
	if err != nil {
		return nil, err
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}

	input := port.IDPAuthInput{
		RedirectURI: redirectURI,
		Scopes:      extraScopes,
		State:       state,
		Nonce:       nonce,
	}

	return s.provider.AuthorizationURL(ctx, input)
}

// HandleCallback processes the IDP callback, resolves the local user, and returns available tenants.
func (s *IDPService) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	result, err := s.provider.HandleCallback(ctx, input)
	if err != nil {
		s.emit(ctx, types.IAMEvent{
			Type:      types.EventIDPLoginFailed,
			Provider:  s.provider.ProviderName(),
			Timestamp: time.Now(),
			Error:     err,
		})
		return nil, err
	}

	// Resolve the local user via the external identity.
	identity := types.ExternalIdentity{
		Provider:   result.IDPUser.Provider,
		ExternalID: result.IDPUser.ExternalID,
		Email:      result.IDPUser.Email,
		LinkedAt:   time.Now(),
	}

	user, err := s.user.FindOrCreateByExternalIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}

	tenants, err := s.tenant.ListUserTenants(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	eventType := types.EventIDPLogin
	if result.IsNewUser {
		eventType = types.EventNewUser
	}
	s.emit(ctx, types.IAMEvent{
		Type:      eventType,
		UserID:    user.ID,
		Provider:  s.provider.ProviderName(),
		Timestamp: time.Now(),
	})

	return &port.IDPCallbackResult{
		IDPUser:   result.IDPUser,
		Tenants:   tenants,
		IsNewUser: result.IsNewUser,
	}, nil
}

// SelectTenant creates a session after IDP login, reusing the same logic as the local flow.
func (s *IDPService) SelectTenant(ctx context.Context, input port.SelectTenantInput) (*types.Session, error) {
	if err := s.tenant.AssertAccess(ctx, input.UserID, input.TenantID, input.Module); err != nil {
		return nil, ErrTenantAccessDenied
	}

	tenants, err := s.tenant.ListUserTenants(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	roles := rolesForTenant(tenants, input.TenantID)

	sessionID := uuid.New().String()
	now := time.Now()

	claims := types.Claims{
		UserID:       input.UserID,
		TenantID:     input.TenantID,
		Module:       input.Module,
		Roles:        roles,
		SessionID:    sessionID,
		AuthProvider: s.provider.ProviderName(),
		Issuer:       s.cfg.issuer,
		IssuedAt:     now,
		ExpiresAt:    now.Add(s.cfg.accessTokenTTL),
	}

	accessToken, err := s.token.IssueAccessToken(claims)
	if err != nil {
		return nil, err
	}

	refreshToken, err := buildRefreshToken(sessionID)
	if err != nil {
		return nil, err
	}

	session := &types.Session{
		ID:                   sessionID,
		UserID:               input.UserID,
		TenantID:             input.TenantID,
		Module:               input.Module,
		AccessToken:          accessToken,
		RefreshToken:         refreshToken,
		RefreshTokenHash:     hashToken(refreshToken),
		RefreshTokenLastFour: lastFour(refreshToken),
		AuthProvider:         s.provider.ProviderName(),
		ExpiresAt:            now.Add(s.cfg.refreshTokenTTL),
		CreatedAt:            now,
		LastUsedAt:           now,
		IPAddress:            input.IPAddress,
		UserAgent:            input.UserAgent,
		DeviceID:             input.DeviceID,
	}

	if err := s.session.Save(ctx, session); err != nil {
		return nil, err
	}

	s.emit(ctx, types.IAMEvent{
		Type:      types.EventTenantSelected,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Module:    input.Module,
		SessionID: sessionID,
		Provider:  s.provider.ProviderName(),
		Timestamp: now,
	})

	return session, nil
}

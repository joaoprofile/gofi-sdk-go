package core

import (
	"context"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
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

	// Tickets makes tenant tickets single-use; nil keeps them reusable until expiry.
	Tickets port.TicketStore
}

// IDPService orchestrates the social login flow (OAuth2/OIDC) for a specific provider.
type IDPService struct {
	sessionIssuer
	provider port.IDPAuthPort
	user     port.UserPort
}

// NewIDPService builds an IDPService for the given provider.
func NewIDPService(cfg IDPServiceConfig) *IDPService {
	return &IDPService{
		sessionIssuer: newSessionIssuer(cfg.Tenant, cfg.Token, cfg.Session, cfg.Cfg, cfg.Tickets, cfg.Emit),
		provider:      cfg.Provider,
		user:          cfg.User,
	}
}

// InitFlow prepares the OAuth2/OIDC flow by generating state, PKCE, and returning the authorization URL.
// The caller must store IDPAuthURL.State and CodeVerifier in an HttpOnly cookie.
func (s *IDPService) InitFlow(ctx context.Context, redirectURI string, extraScopes []string) (*port.IDPAuthURL, error) {
	state, err := generateState()
	if err != nil {
		return nil, err
	}

	input := port.IDPAuthInput{
		RedirectURI: redirectURI,
		Scopes:      extraScopes,
		State:       state,
		Nonce:       NonceForState(state),
	}

	return s.provider.AuthorizationURL(ctx, input)
}

// HandleCallback processes the IDP callback, resolves the local user, and returns available tenants.
func (s *IDPService) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	if s.user == nil || s.tenant == nil {
		return nil, ErrLoginPortsRequired
	}
	// Equal empty states would pass any comparison: that is login CSRF.
	if input.State == "" || input.ExpectedState == "" {
		s.emit(ctx, types.IAMEvent{
			Type:      types.EventIDPLoginFailed,
			Provider:  s.provider.ProviderName(),
			Timestamp: time.Now(),
			Error:     ErrInvalidIDPState,
		})
		return nil, ErrInvalidIDPState
	}
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
		Provider:      result.IDPUser.Provider,
		ExternalID:    result.IDPUser.ExternalID,
		Email:         result.IDPUser.Email,
		EmailVerified: result.IDPUser.EmailVerified,
		LinkedAt:      time.Now(),
	}

	user, err := s.user.FindOrCreateByExternalIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.Active {
		var uid string
		if user != nil {
			uid = user.ID
		}
		s.emit(ctx, types.IAMEvent{
			Type:      types.EventIDPLoginFailed,
			UserID:    uid,
			Provider:  s.provider.ProviderName(),
			Timestamp: time.Now(),
			Error:     ErrAccountInactive,
		})
		return nil, ErrAccountInactive
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
		UserID:    user.ID,
		Ticket:    s.cfg.issueTenantTicket(s.provider.ProviderName(), user.ID),
		IDPUser:   result.IDPUser,
		Tenants:   tenants,
		IsNewUser: result.IsNewUser,
	}, nil
}

// SelectTenant creates a session after IDP login, reusing the same logic as the local flow.
func (s *IDPService) SelectTenant(ctx context.Context, input port.SelectTenantInput) (*types.Session, error) {
	return s.selectTenant(ctx, s.provider.ProviderName(), input)
}

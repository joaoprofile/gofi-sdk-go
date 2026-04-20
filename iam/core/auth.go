package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
)

// AuthConfig holds token lifetime and issuer identification settings.
type AuthConfig struct {
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	issuer          string
}

// LocalAuthConfig holds the parameters for building the built-in AuthPort.
type LocalAuthConfig struct {
	User    port.UserPort
	Tenant  port.TenantPort
	Token   port.TokenPort
	Session port.SessionPort
	Cfg     AuthConfig
	Emit    func(context.Context, types.IAMEvent)
}

// localAuth is the built-in implementation of port.AuthPort using UserPort and TenantPort.
// Activated when no custom AuthPort is provided in Config.
type localAuth struct {
	user    port.UserPort
	tenant  port.TenantPort
	token   port.TokenPort
	session port.SessionPort
	cfg     AuthConfig
	emit    func(context.Context, types.IAMEvent)
}

// NewLocalAuth builds the built-in AuthPort.
func NewLocalAuth(cfg LocalAuthConfig) port.AuthPort {
	emit := cfg.Emit
	if emit == nil {
		emit = func(context.Context, types.IAMEvent) {}
	}
	return &localAuth{
		user:    cfg.User,
		tenant:  cfg.Tenant,
		token:   cfg.Token,
		session: cfg.Session,
		cfg:     cfg.Cfg,
		emit:    emit,
	}
}

// Authenticate validates credentials and returns available tenants.
// Does not issue tokens — the caller must invoke SelectTenant to obtain a session.
func (a *localAuth) Authenticate(ctx context.Context, input port.AuthInput) (*port.AuthResult, error) {
	user, err := a.user.FindByEmail(ctx, input.Email)
	if err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventLoginFailed,
			Timestamp: time.Now(),
			Error:     ErrInvalidCredentials,
		})
		// Do not distinguish between a non-existent email and a wrong password to prevent user enumeration.
		return nil, ErrInvalidCredentials
	}

	if !user.Active {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventLoginFailed,
			UserID:    user.ID,
			Timestamp: time.Now(),
			Error:     ErrAccountInactive,
		})
		return nil, ErrAccountInactive
	}

	if err := a.user.ValidatePassword(ctx, user.ID, input.Password); err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventLoginFailed,
			UserID:    user.ID,
			Timestamp: time.Now(),
			Error:     ErrInvalidCredentials,
		})
		return nil, ErrInvalidCredentials
	}

	tenants, err := a.tenant.ListUserTenants(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventLogin,
		UserID:    user.ID,
		Provider:  "local",
		Timestamp: time.Now(),
	})

	return &port.AuthResult{
		UserID:  user.ID,
		Tenants: tenants,
	}, nil
}

// SelectTenant validates tenant access, issues tokens, and creates a session.
func (a *localAuth) SelectTenant(ctx context.Context, input port.SelectTenantInput) (*types.Session, error) {
	if err := a.tenant.AssertAccess(ctx, input.UserID, input.TenantID, input.Module); err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTenantAccessDenied,
			UserID:    input.UserID,
			TenantID:  input.TenantID,
			Module:    input.Module,
			Timestamp: time.Now(),
			Error:     ErrTenantAccessDenied,
		})
		return nil, ErrTenantAccessDenied
	}

	tenants, err := a.tenant.ListUserTenants(ctx, input.UserID)
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
		AuthProvider: "local",
		Issuer:       a.cfg.issuer,
		IssuedAt:     now,
		ExpiresAt:    now.Add(a.cfg.accessTokenTTL),
	}

	accessToken, err := a.token.IssueAccessToken(claims)
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
		AuthProvider:         "local",
		ExpiresAt:            now.Add(a.cfg.refreshTokenTTL),
		CreatedAt:            now,
		LastUsedAt:           now,
		IPAddress:            input.IPAddress,
		UserAgent:            input.UserAgent,
		DeviceID:             input.DeviceID,
	}

	if err := a.session.Save(ctx, session); err != nil {
		return nil, err
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventTenantSelected,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Module:    input.Module,
		SessionID: sessionID,
		Provider:  "local",
		IPAddress: input.IPAddress,
		UserAgent: input.UserAgent,
		DeviceID:  input.DeviceID,
		Timestamp: now,
	})

	return session, nil
}

// Logout revokes the session — a real logout independent of token expiry.
func (a *localAuth) Logout(ctx context.Context, sessionID string) error {
	if err := a.session.Revoke(ctx, sessionID); err != nil {
		return err
	}
	a.emit(ctx, types.IAMEvent{
		Type:      types.EventLogout,
		SessionID: sessionID,
		Timestamp: time.Now(),
	})
	return nil
}

// RefreshToken renews the access token with mandatory rotation.
// Detects reuse of a revoked refresh token as a possible compromise signal.
func (a *localAuth) RefreshToken(ctx context.Context, refreshToken string) (*types.Session, error) {
	sessionID, err := parseRefreshToken(refreshToken)
	if err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTokenRefreshFailed,
			Timestamp: time.Now(),
			Error:     err,
		})
		return nil, ErrTokenInvalid
	}

	existing, err := a.session.Get(ctx, sessionID)
	if err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTokenRefreshFailed,
			SessionID: sessionID,
			Timestamp: time.Now(),
			Error:     err,
		})
		return nil, ErrSessionNotFound
	}

	// Refresh token reuse detection — possible token theft.
	if existing.Revoked {
		a.session.RevokeAllForUser(ctx, existing.UserID) //nolint:errcheck
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventSuspiciousActivity,
			UserID:    existing.UserID,
			SessionID: sessionID,
			Timestamp: time.Now(),
			Extra:     map[string]any{"reason": "refresh_token_reuse"},
		})
		return nil, ErrSessionRevoked
	}

	if time.Now().After(existing.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	// Validate the refresh token hash.
	if hashToken(refreshToken) != existing.RefreshTokenHash {
		a.session.RevokeAllForUser(ctx, existing.UserID) //nolint:errcheck
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventSuspiciousActivity,
			UserID:    existing.UserID,
			SessionID: sessionID,
			Timestamp: time.Now(),
			Extra:     map[string]any{"reason": "refresh_token_hash_mismatch"},
		})
		return nil, ErrTokenInvalid
	}

	// Revoke the current session — mandatory rotation.
	if err := a.session.Revoke(ctx, sessionID); err != nil {
		return nil, err
	}

	// Create a new session with new tokens.
	newSessionID := uuid.New().String()
	now := time.Now()

	claims := types.Claims{
		UserID:       existing.UserID,
		TenantID:     existing.TenantID,
		Module:       existing.Module,
		SessionID:    newSessionID,
		AuthProvider: existing.AuthProvider,
		Issuer:       a.cfg.issuer,
		IssuedAt:     now,
		ExpiresAt:    now.Add(a.cfg.accessTokenTTL),
	}

	accessToken, err := a.token.IssueAccessToken(claims)
	if err != nil {
		return nil, err
	}

	newRefreshToken, err := buildRefreshToken(newSessionID)
	if err != nil {
		return nil, err
	}

	newSession := &types.Session{
		ID:                   newSessionID,
		UserID:               existing.UserID,
		TenantID:             existing.TenantID,
		Module:               existing.Module,
		AccessToken:          accessToken,
		RefreshToken:         newRefreshToken,
		RefreshTokenHash:     hashToken(newRefreshToken),
		RefreshTokenLastFour: lastFour(newRefreshToken),
		AuthProvider:         existing.AuthProvider,
		ExpiresAt:            now.Add(a.cfg.refreshTokenTTL),
		CreatedAt:            now,
		LastUsedAt:           now,
		IPAddress:            existing.IPAddress,
		UserAgent:            existing.UserAgent,
		DeviceID:             existing.DeviceID,
	}

	if err := a.session.Save(ctx, newSession); err != nil {
		return nil, err
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventTokenRefreshed,
		UserID:    existing.UserID,
		TenantID:  existing.TenantID,
		SessionID: newSessionID,
		Provider:  existing.AuthProvider,
		Timestamp: now,
	})

	return newSession, nil
}

// ValidateToken validates the access token signature and expiry, then verifies the session.
func (a *localAuth) ValidateToken(ctx context.Context, accessToken string) (*types.Claims, error) {
	claims, err := a.token.ParseToken(accessToken)
	if err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTokenInvalid,
			Timestamp: time.Now(),
			Error:     err,
		})
		return nil, err
	}

	session, err := a.session.Get(ctx, claims.SessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}

	if session.Revoked {
		return nil, ErrSessionRevoked
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventTokenValidated,
		UserID:    claims.UserID,
		TenantID:  claims.TenantID,
		SessionID: claims.SessionID,
		Timestamp: time.Now(),
	})

	return claims, nil
}

// rolesForTenant extracts the user's roles for a specific tenant.
func rolesForTenant(tenants []types.TenantAccess, tenantID string) []string {
	for _, t := range tenants {
		if t.Tenant.ID == tenantID {
			return t.Roles
		}
	}
	return nil
}

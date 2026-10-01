package core

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

// AuthConfig holds token lifetime and issuer identification settings.
type AuthConfig struct {
	accessTokenTTL     time.Duration
	refreshTokenTTL    time.Duration
	sessionMaxLifetime time.Duration
	issuer             string
	ticketKey          []byte
	skipTicket         bool
	dummyPasswordHash  string
	tickets            port.TicketStore
}

// sessionExpiry returns the refresh deadline for a session issued at now,
// capped by the absolute lifetime counted from authTime (the original login).
func (c AuthConfig) sessionExpiry(now, authTime time.Time) time.Time {
	maxLife := c.sessionMaxLifetime
	if maxLife <= 0 {
		maxLife = c.refreshTokenTTL
	}
	exp := now.Add(c.refreshTokenTTL)
	if limit := authTime.Add(maxLife); limit.Before(exp) {
		return limit
	}
	return exp
}

// LocalAuthConfig holds the parameters for building the built-in AuthPort.
type LocalAuthConfig struct {
	User    port.UserPort
	Tenant  port.TenantPort
	Token   port.TokenPort
	Session port.SessionPort
	Cfg     AuthConfig
	Emit    func(context.Context, types.IAMEvent)

	// Throttler limits password guessing; nil disables throttling.
	Throttler port.LoginThrottler
	// Tickets makes tenant tickets single-use; nil keeps them reusable until expiry.
	Tickets port.TicketStore
}

// localProvider is the AuthProvider of sessions opened by the built-in local login.
const localProvider = "local"

// localAuth is the built-in implementation of port.AuthPort using UserPort and TenantPort.
// Activated when no custom AuthPort is provided in Config.
type localAuth struct {
	sessionIssuer
	user     port.UserPort
	throttle port.LoginThrottler
}

// NewLocalAuth builds the built-in AuthPort.
func NewLocalAuth(cfg LocalAuthConfig) port.AuthPort {
	return &localAuth{
		sessionIssuer: newSessionIssuer(cfg.Tenant, cfg.Token, cfg.Session, cfg.Cfg, cfg.Tickets, cfg.Emit),
		user:          cfg.User,
		throttle:      cfg.Throttler,
	}
}

// Authenticate validates credentials and returns available tenants.
// Does not issue tokens — the caller must invoke SelectTenant to obtain a session.
func (a *localAuth) Authenticate(ctx context.Context, input port.AuthInput) (*port.AuthResult, error) {
	if a.user == nil || a.tenant == nil {
		return nil, ErrLoginPortsRequired
	}
	attempt := port.LoginAttempt{Email: normalizeEmail(input.Email), IPAddress: input.IPAddress}
	// Before any lookup or hashing, so a locked-out guesser costs almost nothing.
	if err := a.allowLogin(ctx, attempt); err != nil {
		return nil, err
	}
	user, err := a.user.FindByEmail(ctx, input.Email)
	if err != nil || user == nil {
		// Same error and similar latency as a wrong password, to prevent user enumeration.
		a.dummyPasswordCheck(ctx, input.Password)
		return nil, a.loginFailed(ctx, attempt, "", ErrInvalidCredentials)
	}
	if err := a.user.ValidatePassword(ctx, user.ID, input.Password); err != nil {
		return nil, a.loginFailed(ctx, attempt, user.ID, ErrInvalidCredentials)
	}
	a.loginSucceeded(ctx, attempt)

	// Checked after the password so account status is not revealed to guessers.
	if !user.Active {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventLoginFailed,
			UserID:    user.ID,
			IPAddress: input.IPAddress,
			Timestamp: time.Now(),
			Error:     ErrAccountInactive,
		})
		return nil, ErrAccountInactive
	}

	tenants, err := a.tenant.ListUserTenants(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventLogin,
		UserID:    user.ID,
		Provider:  localProvider,
		IPAddress: input.IPAddress,
		Timestamp: time.Now(),
	})

	return &port.AuthResult{
		UserID:  user.ID,
		Tenants: tenants,
		Ticket:  a.cfg.issueTenantTicket(localProvider, user.ID),
	}, nil
}

// normalizeEmail makes throttling keys case- and whitespace-insensitive.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// allowLogin consults the throttler. A store failure fails closed.
func (a *localAuth) allowLogin(ctx context.Context, at port.LoginAttempt) error {
	if a.throttle == nil {
		return nil
	}
	err := a.throttle.Allow(ctx, at)
	if err == nil {
		return nil
	}
	a.emit(ctx, types.IAMEvent{
		Type:      types.EventLoginThrottled,
		IPAddress: at.IPAddress,
		Timestamp: time.Now(),
		Error:     err,
	})
	if errors.Is(err, ErrTooManyAttempts) {
		return ErrTooManyAttempts
	}
	return fmt.Errorf("iam: login throttler: %w", err)
}

// loginFailed records the failure with the throttler and returns cause.
func (a *localAuth) loginFailed(ctx context.Context, at port.LoginAttempt, userID string, cause error) error {
	ev := types.IAMEvent{
		Type:      types.EventLoginFailed,
		UserID:    userID,
		IPAddress: at.IPAddress,
		Timestamp: time.Now(),
		Error:     cause,
	}
	if a.throttle != nil {
		if err := a.throttle.Failure(ctx, at); err != nil {
			ev.Extra = map[string]any{"throttler_error": err.Error()}
		}
	}
	a.emit(ctx, ev)
	return cause
}

// loginSucceeded clears the email's failures once the password matched.
func (a *localAuth) loginSucceeded(ctx context.Context, at port.LoginAttempt) {
	if a.throttle == nil {
		return
	}
	if err := a.throttle.Success(ctx, at); err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventLoginThrottled,
			IPAddress: at.IPAddress,
			Timestamp: time.Now(),
			Error:     err,
		})
	}
}

// SelectTenant validates tenant access, issues tokens, and creates a session.
func (a *localAuth) SelectTenant(ctx context.Context, input port.SelectTenantInput) (*types.Session, error) {
	return a.selectTenant(ctx, localProvider, input)
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
// Presenting an already-rotated refresh token is treated as theft and revokes
// every session of the user; the user and tenant access are re-checked.
func (a *localAuth) RefreshToken(ctx context.Context, refreshToken string) (*types.Session, error) {
	if a.user == nil || a.tenant == nil {
		return nil, ErrLoginPortsRequired
	}
	existing, err := a.refreshable(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	if err := a.revalidate(ctx, existing); err != nil {
		return nil, err
	}

	// Resolved before revoking so a tenant lookup failure keeps the current session usable.
	tenants, err := a.tenant.ListUserTenants(ctx, existing.UserID)
	if err != nil {
		return nil, err
	}

	// Revoke the current session — mandatory rotation, atomic when the store supports it.
	if err := a.revokeForRotation(ctx, existing); err != nil {
		return nil, err
	}

	now := time.Now()
	seed := *existing
	seed.AuthTime = sessionAuthTime(existing)
	newSession, err := a.issue(ctx, &seed, rolesForTenant(tenants, existing.TenantID), now)
	if err != nil {
		return nil, err
	}

	a.emit(ctx, types.IAMEvent{
		Type:      types.EventTokenRefreshed,
		UserID:    existing.UserID,
		TenantID:  existing.TenantID,
		SessionID: newSession.ID,
		Provider:  existing.AuthProvider,
		Timestamp: now,
	})

	return newSession, nil
}

// refreshable loads the session of refreshToken and checks it may be rotated.
func (a *localAuth) refreshable(ctx context.Context, refreshToken string) (*types.Session, error) {
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

	// The sid is public (JWT claim): without the secret part nothing may change.
	if !tokenHashMatches(refreshToken, existing.RefreshTokenHash) {
		a.suspicious(ctx, existing, "refresh_token_hash_mismatch", ErrTokenInvalid)
		return nil, ErrTokenInvalid
	}

	// The genuine token of a rotated session came back: possible token theft.
	if existing.Revoked {
		return nil, a.revokeAllOnReuse(ctx, existing)
	}

	now := time.Now()
	if now.After(existing.ExpiresAt) || !a.cfg.sessionExpiry(now, sessionAuthTime(existing)).After(now) {
		return nil, ErrSessionExpired
	}
	if err := a.checkUserCutoff(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// revokeAllOnReuse revokes every session of the user after a refresh token
// reuse; a failure is returned wrapped in ErrSessionRevoked, never swallowed.
func (a *localAuth) revokeAllOnReuse(ctx context.Context, s *types.Session) error {
	err := a.session.RevokeAllForUser(ctx, s.UserID)
	a.suspicious(ctx, s, "refresh_token_reuse", err)
	if err != nil {
		return fmt.Errorf("%w: revoking all sessions of the user: %w", ErrSessionRevoked, err)
	}
	return ErrSessionRevoked
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

	if err := a.checkSession(ctx, claims); err != nil {
		return nil, err
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

// checkSession verifies that the session behind claims is live and belongs to them.
func (a *localAuth) checkSession(ctx context.Context, claims *types.Claims) error {
	session, err := a.session.Get(ctx, claims.SessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	// Defense in depth: a token must not ride on another user's or tenant's session.
	if session.UserID != claims.UserID || session.TenantID != claims.TenantID {
		a.suspicious(ctx, session, "session_claims_mismatch", ErrTokenInvalid)
		return ErrTokenInvalid
	}
	if session.Revoked {
		return ErrSessionRevoked
	}
	if time.Now().After(session.ExpiresAt) {
		return ErrSessionExpired
	}
	return a.checkUserCutoff(ctx, session)
}

// revalidate re-checks the user and the tenant access at refresh time, so a
// deactivated user or one removed from the tenant cannot keep refreshing.
func (a *localAuth) revalidate(ctx context.Context, s *types.Session) error {
	user, err := a.user.FindByID(ctx, s.UserID)
	if err != nil {
		return err
	}
	if user == nil || !user.Active {
		err := ErrAccountInactive
		if revokeErr := a.session.Revoke(ctx, s.ID); revokeErr != nil {
			err = errors.Join(ErrAccountInactive, revokeErr)
		}
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTokenRefreshFailed,
			UserID:    s.UserID,
			SessionID: s.ID,
			Timestamp: time.Now(),
			Error:     err,
		})
		return err
	}
	if err := a.tenant.AssertAccess(ctx, s.UserID, s.TenantID, s.Module); err != nil {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTenantAccessDenied,
			UserID:    s.UserID,
			TenantID:  s.TenantID,
			Module:    s.Module,
			SessionID: s.ID,
			Timestamp: time.Now(),
			Error:     ErrTenantAccessDenied,
		})
		return ErrTenantAccessDenied
	}
	return nil
}

// sessionAuthTime returns the original login time; sessions stored before
// AuthTime existed fall back to their creation time.
func sessionAuthTime(s *types.Session) time.Time {
	if s.AuthTime.IsZero() {
		return s.CreatedAt
	}
	return s.AuthTime
}

// tokenHashMatches compares the presented token against the stored hash in constant time.
func tokenHashMatches(token, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(storedHash)) == 1
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

// extraToClaims lifts a string-keyed map into the any-typed Extra slot of
// types.Claims. Returns nil when input is empty so JWTs that do not carry
// custom claims stay free of an empty "ext" object.
func extraToClaims(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// copyStringMap returns a defensive copy of the input map. Used to keep the
// session's extras independent of the caller's input map after rotation.
func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// revokeForRotation revokes the session being rotated; a concurrent refresh that
// already revoked it wins, and this one fails without treating it as theft.
func (a *localAuth) revokeForRotation(ctx context.Context, s *types.Session) error {
	r, ok := a.session.(port.SessionRevoker)
	if !ok {
		return a.session.Revoke(ctx, s.ID)
	}
	revoked, err := r.RevokeIfActive(ctx, s.ID)
	if err != nil {
		return err
	}
	if !revoked {
		a.emit(ctx, types.IAMEvent{
			Type:      types.EventTokenRefreshFailed,
			UserID:    s.UserID,
			SessionID: s.ID,
			Timestamp: time.Now(),
			Error:     ErrSessionRevoked,
		})
		return ErrSessionRevoked
	}
	return nil
}

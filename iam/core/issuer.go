package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

// sessionIssuer opens sessions; shared by the local and IDP logins and by refresh.
type sessionIssuer struct {
	tenant  port.TenantPort
	token   port.TokenPort
	session port.SessionPort
	cfg     AuthConfig
	emit    func(context.Context, types.IAMEvent)
}

func newSessionIssuer(tenant port.TenantPort, token port.TokenPort, session port.SessionPort,
	cfg AuthConfig, tickets port.TicketStore, emit func(context.Context, types.IAMEvent)) sessionIssuer {
	if emit == nil {
		emit = func(context.Context, types.IAMEvent) {
			// No event sink configured: events are discarded.
		}
	}
	if tickets != nil {
		cfg.tickets = tickets
	}
	return sessionIssuer{tenant: tenant, token: token, session: session, cfg: cfg, emit: emit}
}

// selectTenant checks the ticket and the tenant access, then opens the session.
// The ticket is consumed only after access is granted, so picking a denied
// tenant does not burn it.
func (s sessionIssuer) selectTenant(ctx context.Context, provider string, input port.SelectTenantInput) (*types.Session, error) {
	if s.tenant == nil {
		return nil, ErrLoginPortsRequired
	}
	ticket, err := s.cfg.checkTenantTicket(input.Ticket, provider, input.UserID)
	if err != nil {
		s.denied(ctx, provider, input, err)
		return nil, err
	}
	if err := s.tenant.AssertAccess(ctx, input.UserID, input.TenantID, input.Module); err != nil {
		s.denied(ctx, provider, input, ErrTenantAccessDenied)
		return nil, ErrTenantAccessDenied
	}
	if err := s.cfg.consumeTicket(ctx, ticket); err != nil {
		s.denied(ctx, provider, input, err)
		return nil, err
	}

	tenants, err := s.tenant.ListUserTenants(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	seed := &types.Session{
		UserID:       input.UserID,
		TenantID:     input.TenantID,
		Module:       input.Module,
		AuthProvider: provider,
		AuthTime:     now,
		IPAddress:    input.IPAddress,
		UserAgent:    input.UserAgent,
		DeviceID:     input.DeviceID,
		ClaimsExtra:  input.ClaimsExtra,
		SessionExtra: input.SessionExtra,
	}
	session, err := s.issue(ctx, seed, rolesForTenant(tenants, input.TenantID), now)
	if err != nil {
		return nil, err
	}

	s.emit(ctx, types.IAMEvent{
		Type:      types.EventTenantSelected,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Module:    input.Module,
		SessionID: session.ID,
		Provider:  provider,
		IPAddress: input.IPAddress,
		UserAgent: input.UserAgent,
		DeviceID:  input.DeviceID,
		Timestamp: now,
	})
	return session, nil
}

func (s sessionIssuer) denied(ctx context.Context, provider string, input port.SelectTenantInput, err error) {
	s.emit(ctx, types.IAMEvent{
		Type:      types.EventTenantAccessDenied,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Module:    input.Module,
		Provider:  provider,
		IPAddress: input.IPAddress,
		Timestamp: time.Now(),
		Error:     err,
	})
}

// issue creates a new session from seed (identity, context, AuthTime and
// extras), issues its tokens and saves it. Only ClaimsExtra reaches the token.
func (s sessionIssuer) issue(ctx context.Context, seed *types.Session, roles []string, now time.Time) (*types.Session, error) {
	sessionID := uuid.New().String()
	accessToken, err := s.token.IssueAccessToken(types.Claims{
		UserID:       seed.UserID,
		TenantID:     seed.TenantID,
		Module:       seed.Module,
		Roles:        roles,
		SessionID:    sessionID,
		AuthProvider: seed.AuthProvider,
		Issuer:       s.cfg.issuer,
		IssuedAt:     now,
		ExpiresAt:    now.Add(s.cfg.accessTokenTTL),
		Extra:        extraToClaims(seed.ClaimsExtra),
	})
	if err != nil {
		return nil, err
	}

	refreshToken, err := buildRefreshToken(sessionID)
	if err != nil {
		return nil, err
	}

	session := &types.Session{
		ID:                   sessionID,
		UserID:               seed.UserID,
		TenantID:             seed.TenantID,
		Module:               seed.Module,
		AccessToken:          accessToken,
		RefreshToken:         refreshToken,
		RefreshTokenHash:     hashToken(refreshToken),
		RefreshTokenLastFour: lastFour(refreshToken),
		AuthProvider:         seed.AuthProvider,
		AuthTime:             seed.AuthTime,
		ExpiresAt:            s.cfg.sessionExpiry(now, seed.AuthTime),
		CreatedAt:            now,
		LastUsedAt:           now,
		IPAddress:            seed.IPAddress,
		UserAgent:            seed.UserAgent,
		DeviceID:             seed.DeviceID,
		ClaimsExtra:          copyStringMap(seed.ClaimsExtra),
		SessionExtra:         copyStringMap(seed.SessionExtra),
	}
	if err := s.session.Save(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// checkUserCutoff rejects a session whose login is not after the user's
// RevokeAllForUser cut-off, when the store records one.
func (s sessionIssuer) checkUserCutoff(ctx context.Context, sess *types.Session) error {
	r, ok := s.session.(port.UserRevocationStore)
	if !ok {
		return nil
	}
	cutoff, err := r.RevokedBefore(ctx, sess.UserID)
	if err != nil {
		return err
	}
	if !cutoff.IsZero() && !sessionAuthTime(sess).After(cutoff) {
		return ErrSessionRevoked
	}
	return nil
}

// suspicious emits EventSuspiciousActivity for session s.
func (s sessionIssuer) suspicious(ctx context.Context, sess *types.Session, reason string, err error) {
	s.emit(ctx, types.IAMEvent{
		Type:      types.EventSuspiciousActivity,
		UserID:    sess.UserID,
		SessionID: sess.ID,
		Timestamp: time.Now(),
		Error:     err,
		Extra:     map[string]any{"reason": reason},
	})
}

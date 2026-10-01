package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eventLog struct{ events []types.IAMEvent }

func (l *eventLog) emit(_ context.Context, e types.IAMEvent) { l.events = append(l.events, e) }

func (l *eventLog) reasons() []any {
	var out []any
	for _, e := range l.events {
		if e.Type == types.EventSuspiciousActivity {
			out = append(out, e.Extra["reason"])
		}
	}
	return out
}

type securityFixture struct {
	auth   port.AuthPort
	sess   *memSession
	user   *stubUserPort
	tenant *stubTenantPort
	log    *eventLog
}

func newSecurityFixture(cfg AuthConfig) *securityFixture {
	f := &securityFixture{
		sess:   newMemSession(),
		user:   &stubUserPort{user: &types.User{ID: "u1", Active: true}},
		tenant: &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}},
		log:    &eventLog{},
	}
	if cfg.accessTokenTTL == 0 {
		cfg.accessTokenTTL = time.Minute
	}
	if cfg.refreshTokenTTL == 0 {
		cfg.refreshTokenTTL = time.Hour
	}
	cfg.ticketKey = ticketKey
	f.auth = NewLocalAuth(LocalAuthConfig{
		User: f.user, Tenant: f.tenant, Token: &stubTokenPort{accessToken: "at"},
		Session: f.sess, Cfg: cfg, Emit: f.log.emit,
	})
	return f
}

func (f *securityFixture) login(t *testing.T) *types.Session {
	t.Helper()
	s, err := f.auth.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	require.NoError(t, err)
	return s
}

func (f *securityFixture) stored(t *testing.T, id string) *types.Session {
	t.Helper()
	s, err := f.sess.Get(context.Background(), id)
	require.NoError(t, err)
	return s
}

// Attack: the sid is public (JWT claim); "<sid>.garbage" must not log the victim out everywhere.
func TestRefresh_ForgedTokenOnActiveSessionDoesNotRevokeAll(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	victim, other := f.login(t), f.login(t)

	_, err := f.auth.RefreshToken(context.Background(), victim.ID+".forged")
	assert.ErrorIs(t, err, ErrTokenInvalid)

	assert.False(t, f.stored(t, victim.ID).Revoked)
	assert.False(t, f.stored(t, other.ID).Revoked)
	assert.Equal(t, []any{"refresh_token_hash_mismatch"}, f.log.reasons())
}

// Attack: forged token for an already-rotated (revoked) sid must not trigger reuse detection.
func TestRefresh_ForgedTokenOnRevokedSessionDoesNotRevokeAll(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	old := f.login(t)
	rotated, err := f.auth.RefreshToken(context.Background(), old.RefreshToken)
	require.NoError(t, err)

	_, err = f.auth.RefreshToken(context.Background(), old.ID+".forged")
	assert.ErrorIs(t, err, ErrTokenInvalid)
	assert.False(t, f.stored(t, rotated.ID).Revoked, "live session survives")
	assert.Equal(t, []any{"refresh_token_hash_mismatch"}, f.log.reasons())
}

func TestRefresh_GenuineReuseOfRotatedTokenRevokesAll(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	old := f.login(t)
	rotated, err := f.auth.RefreshToken(context.Background(), old.RefreshToken)
	require.NoError(t, err)

	_, err = f.auth.RefreshToken(context.Background(), old.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionRevoked)
	assert.True(t, f.stored(t, rotated.ID).Revoked)
	assert.Equal(t, []any{"refresh_token_reuse"}, f.log.reasons())
}

func TestRefresh_DeactivatedUserIsRejectedAndSessionRevoked(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	s := f.login(t)
	f.user.user = &types.User{ID: "u1", Active: false}

	_, err := f.auth.RefreshToken(context.Background(), s.RefreshToken)
	assert.ErrorIs(t, err, ErrAccountInactive)
	assert.True(t, f.stored(t, s.ID).Revoked)
}

func TestRefresh_UserLookupErrorKeepsSession(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	s := f.login(t)
	f.user.findErr = errors.New("db down")

	_, err := f.auth.RefreshToken(context.Background(), s.RefreshToken)
	assert.Error(t, err)
	assert.False(t, f.stored(t, s.ID).Revoked)
}

func TestRefresh_LostTenantAccessIsRejected(t *testing.T) {
	f := newSecurityFixture(AuthConfig{})
	s := f.login(t)
	f.tenant.accessErr = errors.New("removed from tenant")

	_, err := f.auth.RefreshToken(context.Background(), s.RefreshToken)
	assert.ErrorIs(t, err, ErrTenantAccessDenied)
}

func TestRefresh_RequiresUserPort(t *testing.T) {
	auth := NewLocalAuth(LocalAuthConfig{Tenant: &stubTenantPort{}, Session: newMemSession()})
	_, err := auth.RefreshToken(context.Background(), "sid.x")
	assert.ErrorIs(t, err, ErrLoginPortsRequired)
}

func TestSession_AbsoluteLifetimeCapsSlidingRefresh(t *testing.T) {
	f := newSecurityFixture(AuthConfig{refreshTokenTTL: 7 * 24 * time.Hour, sessionMaxLifetime: time.Hour})
	s := f.login(t)
	assert.WithinDuration(t, s.AuthTime.Add(time.Hour), s.ExpiresAt, time.Second)

	rotated, err := f.auth.RefreshToken(context.Background(), s.RefreshToken)
	require.NoError(t, err)
	assert.True(t, rotated.AuthTime.Equal(s.AuthTime), "AuthTime inherited across rotation")
	assert.False(t, rotated.ExpiresAt.After(s.AuthTime.Add(time.Hour)))

	// Login happened two hours ago: the session may not be extended any more.
	f.sess.sessions[rotated.ID].AuthTime = time.Now().Add(-2 * time.Hour)
	_, err = f.auth.RefreshToken(context.Background(), rotated.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestSession_LegacySessionWithoutAuthTimeUsesCreatedAt(t *testing.T) {
	f := newSecurityFixture(AuthConfig{sessionMaxLifetime: time.Hour})
	s := f.login(t)
	stored := f.sess.sessions[s.ID]
	stored.AuthTime = time.Time{}
	stored.CreatedAt = time.Now().Add(-2 * time.Hour)

	_, err := f.auth.RefreshToken(context.Background(), s.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionExpired)
}

func TestSessionExpiry_ZeroMaxLifetimeFallsBackToRefreshTTL(t *testing.T) {
	now := time.Now()
	c := AuthConfig{refreshTokenTTL: time.Hour}
	assert.Equal(t, now.Add(time.Hour), c.sessionExpiry(now, now))
	assert.Equal(t, now.Add(30*time.Minute), c.sessionExpiry(now, now.Add(-30*time.Minute)))
}

// Attack: login CSRF with an empty state parameter and no state cookie.
func TestIDPService_EmptyStateIsRejected(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google", callbackResult: &port.IDPCallbackResult{}}
	svc := buildIDPService(idp, &stubUserPort{externalUser: &types.User{ID: "u1", Active: true}}, &stubTenantPort{}, &stubTokenPort{})

	for _, in := range []port.IDPCallbackInput{{}, {State: "s"}, {ExpectedState: "s"}} {
		_, err := svc.HandleCallback(context.Background(), in)
		assert.ErrorIs(t, err, ErrInvalidIDPState)
	}
}

func TestIDPService_InactiveUserCannotLogIn(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google", callbackResult: &port.IDPCallbackResult{}}
	for _, u := range []*types.User{{ID: "u1", Active: false}, nil} {
		svc := buildIDPService(idp, &stubUserPort{externalUser: u}, &stubTenantPort{}, &stubTokenPort{})
		res, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{State: "s", ExpectedState: "s"})
		assert.ErrorIs(t, err, ErrAccountInactive)
		assert.Nil(t, res, "no ticket for an inactive user")
	}
}

type identityCapture struct {
	stubUserPort
	got types.ExternalIdentity
}

func (c *identityCapture) FindOrCreateByExternalIdentity(_ context.Context, id types.ExternalIdentity) (*types.User, error) {
	c.got = id
	return &types.User{ID: "u1", Active: true}, nil
}

func TestIDPService_PassesEmailVerifiedToUserPort(t *testing.T) {
	for _, verified := range []bool{true, false} {
		idp := &stubIDPAuthPort{name: "google", callbackResult: &port.IDPCallbackResult{
			IDPUser: types.IDPUser{Provider: "google", ExternalID: "x", Email: "a@b.com", EmailVerified: verified},
		}}
		users := &identityCapture{}
		svc := NewIDPService(IDPServiceConfig{Provider: idp, User: users, Tenant: &stubTenantPort{}, Cfg: AuthConfig{ticketKey: ticketKey}})
		_, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{State: "s", ExpectedState: "s"})
		require.NoError(t, err)
		assert.Equal(t, verified, users.got.EmailVerified)
	}
}

func TestIDPService_SessionHasAuthTimeAndCappedExpiry(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google"}
	svc := NewIDPService(IDPServiceConfig{
		Provider: idp, User: &stubUserPort{}, Tenant: &stubTenantPort{}, Token: &stubTokenPort{accessToken: "at"}, Session: newMemSession(),
		Cfg: AuthConfig{accessTokenTTL: time.Minute, refreshTokenTTL: 7 * 24 * time.Hour, sessionMaxLifetime: time.Hour, ticketKey: ticketKey},
	})
	s, err := svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testIDPTicket("google", "u1")})
	require.NoError(t, err)
	assert.False(t, s.AuthTime.IsZero())
	assert.WithinDuration(t, s.AuthTime.Add(time.Hour), s.ExpiresAt, time.Second)
}

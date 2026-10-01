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

// stubThrottler records calls; allowErr is returned by Allow.
type stubThrottler struct {
	allowErr error
	calls    []string
	attempts []port.LoginAttempt
}

func (s *stubThrottler) Allow(_ context.Context, a port.LoginAttempt) error {
	s.calls, s.attempts = append(s.calls, "allow"), append(s.attempts, a)
	return s.allowErr
}

func (s *stubThrottler) Failure(_ context.Context, a port.LoginAttempt) error {
	s.calls, s.attempts = append(s.calls, "failure"), append(s.attempts, a)
	return nil
}

func (s *stubThrottler) Success(_ context.Context, a port.LoginAttempt) error {
	s.calls, s.attempts = append(s.calls, "success"), append(s.attempts, a)
	return nil
}

// countingUser counts lookups so tests can prove Allow runs before them.
type countingUser struct {
	stubUserPort
	lookups int
}

func (u *countingUser) FindByEmail(ctx context.Context, email string) (*types.User, error) {
	u.lookups++
	return u.stubUserPort.FindByEmail(ctx, email)
}

func throttledAuth(user port.UserPort, th port.LoginThrottler, log *eventLog) port.AuthPort {
	return NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: &stubTenantPort{}, Token: &stubTokenPort{}, Session: newMemSession(),
		Cfg: AuthConfig{ticketKey: ticketKey}, Emit: log.emit, Throttler: th,
	})
}

// Attack: unlimited password guessing against one account.
func TestAuthenticate_LockedOutBeforeLookupAndHashing(t *testing.T) {
	th := &stubThrottler{allowErr: ErrTooManyAttempts}
	user := &countingUser{stubUserPort: stubUserPort{user: &types.User{ID: "u1", Active: true}}}
	log := &eventLog{}

	_, err := throttledAuth(user, th, log).Authenticate(context.Background(),
		port.AuthInput{Email: "  Alice@Example.COM ", Password: "guess", IPAddress: "10.0.0.1"})

	assert.ErrorIs(t, err, ErrTooManyAttempts)
	assert.Zero(t, user.lookups, "no lookup or hashing while locked out")
	assert.Equal(t, []string{"allow"}, th.calls)
	assert.Equal(t, port.LoginAttempt{Email: "alice@example.com", IPAddress: "10.0.0.1"}, th.attempts[0])
	require.NotEmpty(t, log.events)
	assert.Equal(t, types.EventLoginThrottled, log.events[0].Type)
}

func TestAuthenticate_RecordsFailuresAndSuccess(t *testing.T) {
	ctx := context.Background()
	th := &stubThrottler{}
	user := &countingUser{stubUserPort: stubUserPort{findErr: errors.New("not found")}}
	auth := throttledAuth(user, th, &eventLog{})

	_, err := auth.Authenticate(ctx, port.AuthInput{Email: "ghost@x.io", Password: "p"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	user.findErr, user.user, user.validateErr = nil, &types.User{ID: "u1", Active: true}, errors.New("bad")
	_, err = auth.Authenticate(ctx, port.AuthInput{Email: "a@x.io", Password: "p"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	user.validateErr = nil
	_, err = auth.Authenticate(ctx, port.AuthInput{Email: "a@x.io", Password: "p"})
	require.NoError(t, err)

	assert.Equal(t, []string{"allow", "failure", "allow", "failure", "allow", "success"}, th.calls)
}

// A throttler outage fails closed rather than allowing unlimited guessing.
func TestAuthenticate_ThrottlerErrorFailsClosed(t *testing.T) {
	boom := errors.New("redis down")
	user := &countingUser{stubUserPort: stubUserPort{user: &types.User{ID: "u1", Active: true}}}
	_, err := throttledAuth(user, &stubThrottler{allowErr: boom}, &eventLog{}).
		Authenticate(context.Background(), port.AuthInput{Email: "a@x.io", Password: "p"})
	assert.ErrorIs(t, err, boom)
	assert.Zero(t, user.lookups)
}

// cutoffSession adds the optional UserRevocationStore to memSession.
type cutoffSession struct {
	*memSession
	cutoff    time.Time
	cutoffErr error
	revokeErr error
}

func (c *cutoffSession) RevokedBefore(context.Context, string) (time.Time, error) {
	return c.cutoff, c.cutoffErr
}

func (c *cutoffSession) RevokeAllForUser(ctx context.Context, userID string) error {
	if c.revokeErr != nil {
		return c.revokeErr
	}
	c.cutoff = time.Now()
	return c.memSession.RevokeAllForUser(ctx, userID)
}

func cutoffAuth(sess port.SessionPort, tok port.TokenPort, log *eventLog) port.AuthPort {
	return NewLocalAuth(LocalAuthConfig{
		User:   &stubUserPort{user: &types.User{ID: "u1", Active: true}},
		Tenant: &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}},
		Token:  tok, Session: sess, Emit: log.emit,
		Cfg: AuthConfig{ticketKey: ticketKey, accessTokenTTL: time.Minute, refreshTokenTTL: time.Hour},
	})
}

// Attack (race): a refresh running concurrently with LogoutAll saves a new
// session after RevokeAllForUser listed the user's sessions. The cut-off must
// reject it on validation and refresh.
func TestLogoutAll_CutoffRejectsSessionFromConcurrentRefresh(t *testing.T) {
	ctx := context.Background()
	sess := &cutoffSession{memSession: newMemSession()}
	tok := &captureSessionIDTokenPort{accessToken: "at"}
	auth := cutoffAuth(sess, tok, &eventLog{})

	s, err := auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	require.NoError(t, err)
	// The racing refresh's session: same login (AuthTime), saved after the revocation.
	require.NoError(t, sess.RevokeAllForUser(ctx, "u1"))
	late := *s
	late.ID, late.Revoked, late.RevokedAt = "late", false, nil
	late.RefreshToken = "late.secret"
	late.RefreshTokenHash = hashToken(late.RefreshToken)
	require.NoError(t, sess.Save(ctx, &late))

	tok.parseClaims = &types.Claims{UserID: "u1", TenantID: "t1", SessionID: "late", ExpiresAt: time.Now().Add(time.Minute)}
	_, err = auth.ValidateToken(ctx, "at")
	assert.ErrorIs(t, err, ErrSessionRevoked)
	_, err = auth.RefreshToken(ctx, late.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionRevoked)

	// A login after the cut-off works.
	fresh, err := auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	require.NoError(t, err)
	tok.parseClaims.SessionID = fresh.ID
	_, err = auth.ValidateToken(ctx, "at")
	assert.NoError(t, err)

	sess.cutoffErr = errors.New("redis down")
	_, err = auth.ValidateToken(ctx, "at")
	assert.ErrorIs(t, err, sess.cutoffErr, "a cut-off lookup failure fails closed")
}

// RevokeAllForUser failures after a refresh token reuse used to be swallowed.
func TestRefresh_ReuseSurfacesRevokeAllError(t *testing.T) {
	ctx := context.Background()
	sess := &cutoffSession{memSession: newMemSession()}
	log := &eventLog{}
	auth := cutoffAuth(sess, &stubTokenPort{accessToken: "at"}, log)
	old, err := auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	require.NoError(t, err)
	_, err = auth.RefreshToken(ctx, old.RefreshToken)
	require.NoError(t, err)

	sess.revokeErr = errors.New("redis down")
	_, err = auth.RefreshToken(ctx, old.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionRevoked)
	assert.ErrorIs(t, err, sess.revokeErr)
	assert.Equal(t, []any{"refresh_token_reuse"}, log.reasons())
}

// Defense in depth: a token whose sid points at another user's or tenant's session.
func TestValidateToken_RejectsClaimsNotMatchingSession(t *testing.T) {
	ctx := context.Background()
	tok := &captureSessionIDTokenPort{accessToken: "at"}
	log := &eventLog{}
	auth := cutoffAuth(newMemSession(), tok, log)
	s, err := auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	require.NoError(t, err)

	for _, c := range []types.Claims{
		{UserID: "u2", TenantID: "t1", SessionID: s.ID},
		{UserID: "u1", TenantID: "t2", SessionID: s.ID},
	} {
		tok.parseClaims = &c
		_, err = auth.ValidateToken(ctx, "at")
		assert.ErrorIs(t, err, ErrTokenInvalid)
	}
	assert.Equal(t, []any{"session_claims_mismatch", "session_claims_mismatch"}, log.reasons())
}

// The refresh keeps both extras; only ClaimsExtra is re-issued in the token.
func TestRefresh_KeepsExtrasSplit(t *testing.T) {
	ctx := context.Background()
	tok := &stubTokenPort{accessToken: "at"}
	auth := cutoffAuth(newMemSession(), tok, &eventLog{})
	s, err := auth.SelectTenant(ctx, port.SelectTenantInput{
		UserID: "u1", TenantID: "t1", Ticket: testTicket("u1"),
		ClaimsExtra: map[string]string{"role": "ADMIN"}, SessionExtra: map[string]string{"ext": "secret"},
	})
	require.NoError(t, err)
	next, err := auth.RefreshToken(ctx, s.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "secret", next.SessionExtra["ext"])
	assert.Equal(t, map[string]any{"role": "ADMIN"}, tok.issued[len(tok.issued)-1].Extra)
}

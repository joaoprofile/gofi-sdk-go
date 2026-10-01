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

var ticketKey = []byte("0123456789abcdef0123456789abcdef")

// testTicket signs a local-login ticket for userID with the test key.
func testTicket(userID string) string {
	return signTenantTicket(ticketKey, localProvider, userID, time.Now())
}

// testIDPTicket signs a ticket for an IDP login flow.
func testIDPTicket(provider, userID string) string {
	return signTenantTicket(ticketKey, provider, userID, time.Now())
}

func TestTenantTicket_SignVerify(t *testing.T) {
	now := time.Now()
	tk := signTenantTicket(ticketKey, "local", "u1", now)
	verifyTenantTicket := func(key []byte, ticket, provider, userID string, at time.Time) error {
		_, err := verifyTenantTicket(key, ticket, provider, userID, at)
		return err
	}

	assert.NoError(t, verifyTenantTicket(ticketKey, tk, "local", "u1", now.Add(time.Minute)))
	assert.ErrorIs(t, verifyTenantTicket(ticketKey, tk, "local", "u2", now), ErrInvalidTenantTicket, "other user")
	assert.ErrorIs(t, verifyTenantTicket(ticketKey, tk, "google", "u1", now), ErrInvalidTenantTicket, "other login flow")
	assert.ErrorIs(t, verifyTenantTicket(ticketKey, tk, "local", "u1", now.Add(tenantTicketTTL+time.Second)), ErrInvalidTenantTicket, "expired")
	assert.ErrorIs(t, verifyTenantTicket([]byte("another-key-another-key-another-k"), tk, "local", "u1", now), ErrInvalidTenantTicket, "wrong key")
	assert.ErrorIs(t, verifyTenantTicket(ticketKey, tk+"x", "local", "u1", now), ErrInvalidTenantTicket, "tampered")
	assert.ErrorIs(t, verifyTenantTicket(ticketKey, "garbage", "local", "u1", now), ErrInvalidTenantTicket)
	assert.LessOrEqual(t, tenantTicketTTL, 5*time.Minute)
}

// memTickets is a port.TicketStore double.
type memTickets struct {
	seen map[string]time.Duration
	err  error
}

func (m *memTickets) Consume(_ context.Context, jti string, ttl time.Duration) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	if _, ok := m.seen[jti]; ok {
		return false, nil
	}
	m.seen[jti] = ttl
	return true, nil
}

func TestTenantTicket_CarriesUniqueJTI(t *testing.T) {
	now := time.Now()
	a, err := verifyTenantTicket(ticketKey, signTenantTicket(ticketKey, "local", "u1", now), "local", "u1", now)
	require.NoError(t, err)
	b, err := verifyTenantTicket(ticketKey, signTenantTicket(ticketKey, "local", "u1", now), "local", "u1", now)
	require.NoError(t, err)
	assert.NotEmpty(t, a.jti)
	assert.NotEqual(t, a.jti, b.jti)
	assert.Equal(t, now.Add(tenantTicketTTL).Unix(), a.exp.Unix())
}

// Attack: a ticket stolen within its 5 minutes opens a second session.
func TestLocalAuth_TicketIsSingleUseWithStore(t *testing.T) {
	ctx := context.Background()
	store := &memTickets{seen: map[string]time.Duration{}}
	auth := buildTicketAuth(AuthConfig{ticketKey: ticketKey, tickets: store})

	res, err := auth.Authenticate(ctx, port.AuthInput{Email: "a@b.com", Password: "p"})
	require.NoError(t, err)
	in := port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: res.Ticket}
	_, err = auth.SelectTenant(ctx, in)
	require.NoError(t, err)
	_, err = auth.SelectTenant(ctx, in)
	assert.ErrorIs(t, err, ErrInvalidTenantTicket, "reused ticket")
	for _, ttl := range store.seen {
		assert.Greater(t, ttl, tenantTicketTTL-time.Minute, "kept until the ticket expires")
	}
}

// Without a store the documented behaviour stays: reusable until expiry.
func TestLocalAuth_TicketReusableWithoutStore(t *testing.T) {
	ctx := context.Background()
	auth := buildTicketAuth(AuthConfig{ticketKey: ticketKey})
	res, err := auth.Authenticate(ctx, port.AuthInput{Email: "a@b.com", Password: "p"})
	require.NoError(t, err)
	in := port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: res.Ticket}
	_, err = auth.SelectTenant(ctx, in)
	require.NoError(t, err)
	_, err = auth.SelectTenant(ctx, in)
	assert.NoError(t, err)
}

// A denied tenant does not burn the ticket; a store failure fails closed.
func TestLocalAuth_TicketConsumedOnlyAfterAccess(t *testing.T) {
	ctx := context.Background()
	store := &memTickets{seen: map[string]time.Duration{}}
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{accessErr: ErrTenantAccessDenied}
	auth := NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: &stubTokenPort{}, Session: newMemSession(),
		Cfg: AuthConfig{ticketKey: ticketKey, accessTokenTTL: time.Minute, refreshTokenTTL: time.Hour}, Tickets: store,
	})
	_, err := auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t9", Ticket: testTicket("u1")})
	assert.ErrorIs(t, err, ErrTenantAccessDenied)
	assert.Empty(t, store.seen)

	tenant.accessErr = nil
	store.err = errors.New("redis down")
	_, err = auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	assert.ErrorIs(t, err, store.err)
}

func TestIDPService_TicketIsSingleUseWithStore(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google"}
	store := &memTickets{seen: map[string]time.Duration{}}
	svc := NewIDPService(IDPServiceConfig{
		Provider: idp,
		Tenant:   &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}},
		Token:    &stubTokenPort{accessToken: "at"},
		Session:  newMemSession(),
		Cfg:      AuthConfig{accessTokenTTL: time.Minute, refreshTokenTTL: time.Hour, ticketKey: ticketKey},
		Tickets:  store,
	})
	in := port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testIDPTicket("google", "u1")}
	_, err := svc.SelectTenant(context.Background(), in)
	require.NoError(t, err)
	_, err = svc.SelectTenant(context.Background(), in)
	assert.ErrorIs(t, err, ErrInvalidTenantTicket)
}

func buildTicketAuth(cfg AuthConfig) port.AuthPort {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	cfg.accessTokenTTL, cfg.refreshTokenTTL = time.Minute, time.Hour
	return NewLocalAuth(LocalAuthConfig{
		User: user, Tenant: tenant, Token: &stubTokenPort{accessToken: "at"}, Session: newMemSession(), Cfg: cfg,
	})
}

func TestLocalAuth_TenantTicket(t *testing.T) {
	ctx := context.Background()
	auth := buildTicketAuth(AuthConfig{ticketKey: ticketKey})

	res, err := auth.Authenticate(ctx, port.AuthInput{Email: "a@b.com", Password: "p"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Ticket)

	_, err = auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: res.Ticket})
	assert.NoError(t, err)

	_, err = auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "victim", TenantID: "t1", Ticket: res.Ticket})
	assert.ErrorIs(t, err, ErrInvalidTenantTicket, "ticket bound to another user")
}

// Attack: POST /select-tenant with only the victim's user_id must not open a session.
func TestLocalAuth_SelectTenantWithoutTicketIsRejectedByDefault(t *testing.T) {
	ctx := context.Background()
	_, err := buildTicketAuth(AuthConfig{ticketKey: ticketKey}).
		SelectTenant(ctx, port.SelectTenantInput{UserID: "victim", TenantID: "t1"})
	assert.ErrorIs(t, err, ErrInvalidTenantTicket)
}

func TestLocalAuth_NoTicketKeyFailsClosed(t *testing.T) {
	ctx := context.Background()
	auth := buildTicketAuth(AuthConfig{})
	res, err := auth.Authenticate(ctx, port.AuthInput{Email: "a@b.com", Password: "p"})
	require.NoError(t, err)
	assert.Empty(t, res.Ticket)

	for _, tk := range []string{"", "anything", testTicket("u1")} {
		_, err = auth.SelectTenant(ctx, port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: tk})
		assert.ErrorIs(t, err, ErrInvalidTenantTicket, "ticket %q", tk)
	}
}

func TestLocalAuth_InsecureSkipTicket(t *testing.T) {
	_, err := buildTicketAuth(AuthConfig{skipTicket: true}).
		SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1"})
	assert.NoError(t, err)
}

func TestIDPService_CallbackReturnsUserIDAndTicket(t *testing.T) {
	idp := &stubIDPAuthPort{name: "google", callbackResult: &port.IDPCallbackResult{IDPUser: types.IDPUser{Provider: "google", ExternalID: "ext"}}}
	svc := NewIDPService(IDPServiceConfig{
		Provider: idp,
		User:     &stubUserPort{externalUser: &types.User{ID: "u1", Active: true}},
		Tenant:   &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}},
		Token:    &stubTokenPort{accessToken: "at"},
		Session:  newMemSession(),
		Cfg:      AuthConfig{accessTokenTTL: time.Minute, refreshTokenTTL: time.Hour, ticketKey: ticketKey},
	})

	res, err := svc.HandleCallback(context.Background(), port.IDPCallbackInput{State: "s", ExpectedState: "s"})
	require.NoError(t, err)
	assert.Equal(t, "u1", res.UserID)

	_, err = svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: res.Ticket})
	assert.NoError(t, err)
	_, err = svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1"})
	assert.ErrorIs(t, err, ErrInvalidTenantTicket)
	_, err = svc.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", TenantID: "t1", Ticket: testTicket("u1")})
	assert.ErrorIs(t, err, ErrInvalidTenantTicket, "a local-login ticket is not valid for an IDP flow")
}

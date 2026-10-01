package core

import (
	"context"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// racedSession simulates a concurrent refresh winning between Get and revoke.
type racedSession struct{ *memSession }

func (racedSession) RevokeIfActive(context.Context, string) (bool, error) { return false, nil }

func TestRefreshToken_LosingConcurrentRotationFails(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}}}}
	auth, sess := buildLocalAuth(user, tenant, &stubTokenPort{accessToken: "at"})

	session, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", Ticket: testTicket("u1"), TenantID: "t1"})
	require.NoError(t, err)

	auth.(*localAuth).session = racedSession{sess}
	_, err = auth.RefreshToken(context.Background(), session.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionRevoked)
}

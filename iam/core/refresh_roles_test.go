package core

import (
	"context"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshToken_KeepsTenantRoles(t *testing.T) {
	user := &stubUserPort{user: &types.User{ID: "u1", Active: true}}
	tenant := &stubTenantPort{tenants: []types.TenantAccess{{Tenant: types.Tenant{ID: "t1"}, Roles: []string{"admin"}}}}
	token := &stubTokenPort{accessToken: "at"}
	auth, _ := buildLocalAuth(user, tenant, token)

	s, err := auth.SelectTenant(context.Background(), port.SelectTenantInput{UserID: "u1", Ticket: testTicket("u1"), TenantID: "t1"})
	require.NoError(t, err)

	tenant.tenants[0].Roles = []string{"admin", "auditor"} // roles changed since login
	_, err = auth.RefreshToken(context.Background(), s.RefreshToken)
	require.NoError(t, err)

	require.Len(t, token.issued, 2)
	assert.Equal(t, []string{"admin", "auditor"}, token.issued[1].Roles)
}

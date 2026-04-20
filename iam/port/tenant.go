package port

import (
	"context"

	"github.com/joaoprofile/gofi/iam/types"
)

// TenantPort abstracts the tenant repository and multi-tenant access control.
type TenantPort interface {
	// ListUserTenants lists all tenants and modules the user is allowed to access.
	ListUserTenants(ctx context.Context, userID string) ([]types.TenantAccess, error)

	// AssertAccess validates whether the user may access the given tenant and module.
	// Called in SelectTenant and by TenantMiddleware on every request.
	// Token claims must never be the sole source of truth for access decisions.
	AssertAccess(ctx context.Context, userID, tenantID, module string) error
}

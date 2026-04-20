package roles

import (
	"sort"
	"testing"

	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
)

func buildProvider() *Provider {
	return NewRBACProvider(Config{
		Permissions: PermissionMap{
			"admin": {
				"orders":   {"read", "write", "delete"},
				"products": {"read", "write"},
			},
			"viewer": {
				"orders":   {"read"},
				"products": {"read"},
			},
			"editor": {
				"products": {"read", "write"},
			},
			"superadmin": {
				"*": {"*"},
			},
		},
	})
}

func TestEnforce_AllowedAction(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"admin"}}
	assert.True(t, p.Enforce(claims, "orders", "read"))
	assert.True(t, p.Enforce(claims, "orders", "write"))
	assert.True(t, p.Enforce(claims, "orders", "delete"))
}

func TestEnforce_DeniedAction(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"viewer"}}
	assert.False(t, p.Enforce(claims, "orders", "write"))
	assert.False(t, p.Enforce(claims, "orders", "delete"))
}

func TestEnforce_WildcardAction(t *testing.T) {
	p := NewRBACProvider(Config{
		Permissions: PermissionMap{
			"godmode": {
				"resource": {"*"},
			},
		},
	})
	claims := types.Claims{Roles: []string{"godmode"}}
	assert.True(t, p.Enforce(claims, "resource", "anything"))
	assert.True(t, p.Enforce(claims, "resource", "read"))
	assert.True(t, p.Enforce(claims, "resource", "delete"))
}

func TestEnforce_NoRoles(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: nil}
	assert.False(t, p.Enforce(claims, "orders", "read"))
}

func TestEnforce_UnknownRole(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"unknown-role"}}
	assert.False(t, p.Enforce(claims, "orders", "read"))
}

func TestEnforce_UnknownResource(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"admin"}}
	assert.False(t, p.Enforce(claims, "invoices", "read"))
}

func TestEnforce_MultipleRoles_FirstDeniedSecondAllowed(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"viewer", "editor"}}
	// editor has write on products, viewer does not.
	assert.True(t, p.Enforce(claims, "products", "write"))
}

func TestEnforce_EmptyPermissionMap(t *testing.T) {
	p := NewRBACProvider(Config{Permissions: PermissionMap{}})
	claims := types.Claims{Roles: []string{"admin"}}
	assert.False(t, p.Enforce(claims, "resource", "action"))
}

func TestPermissions_SingleRole(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"viewer"}}

	perms := p.Permissions(claims)
	assert.Len(t, perms, 2)

	for _, perm := range perms {
		assert.Equal(t, []string{"read"}, perm.Actions)
	}
}

func TestPermissions_MultipleRoles_Deduplicated(t *testing.T) {
	p := buildProvider()
	// admin and viewer both have "read" on orders and products.
	claims := types.Claims{Roles: []string{"admin", "viewer"}}

	perms := p.Permissions(claims)

	resourceMap := make(map[string][]string)
	for _, perm := range perms {
		resourceMap[perm.Resource] = perm.Actions
	}

	ordersActions := resourceMap["orders"]
	sort.Strings(ordersActions)
	assert.Equal(t, []string{"delete", "read", "write"}, ordersActions)

	productsActions := resourceMap["products"]
	sort.Strings(productsActions)
	assert.Equal(t, []string{"read", "write"}, productsActions)
}

func TestPermissions_NoRoles(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: nil}
	perms := p.Permissions(claims)
	assert.Empty(t, perms)
}

func TestPermissions_UnknownRole(t *testing.T) {
	p := buildProvider()
	claims := types.Claims{Roles: []string{"unknown"}}
	perms := p.Permissions(claims)
	assert.Empty(t, perms)
}

func TestPermissions_EmptyPermissionMap(t *testing.T) {
	p := NewRBACProvider(Config{Permissions: PermissionMap{}})
	claims := types.Claims{Roles: []string{"admin"}}
	perms := p.Permissions(claims)
	assert.Empty(t, perms)
}

func TestPermissions_DuplicateActionsAcrossRolesNotDuplicated(t *testing.T) {
	p := NewRBACProvider(Config{
		Permissions: PermissionMap{
			"r1": {"res": {"read", "write"}},
			"r2": {"res": {"read", "delete"}},
		},
	})
	claims := types.Claims{Roles: []string{"r1", "r2"}}
	perms := p.Permissions(claims)

	require := assert.New(t)
	require.Len(perms, 1)

	actions := perms[0].Actions
	sort.Strings(actions)
	assert.Equal(t, []string{"delete", "read", "write"}, actions)
}

func TestNewRBACProvider_ReturnsProvider(t *testing.T) {
	p := NewRBACProvider(Config{Permissions: PermissionMap{}})
	assert.NotNil(t, p)
}

package types

// Tenant represents an organization or isolated context in the multi-tenant system.
type Tenant struct {
	ID      string
	Name    string
	Modules []string
	Active  bool
}

// TenantAccess represents a user's access to a tenant along with their modules and roles.
type TenantAccess struct {
	Tenant  Tenant
	Modules []string
	Roles   []string
}

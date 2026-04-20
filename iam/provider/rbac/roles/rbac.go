// Package roles implements port.RBACPort with a simple role-to-permissions model.
// For ABAC or policy engines such as OPA or Cedar, implement port.RBACPort directly.
package roles

import (
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
)

// PermissionMap maps role to resource to a list of allowed actions.
type PermissionMap map[string]map[string][]string

// Config configures the role-based RBAC provider.
type Config struct {
	Permissions PermissionMap
}

// Provider implements port.RBACPort with role-based resolution from claims.
type Provider struct {
	perms PermissionMap
}

// NewRBACProvider builds an RBAC Provider with the given permission map.
func NewRBACProvider(cfg Config) *Provider {
	return &Provider{perms: cfg.Permissions}
}

// Enforce checks whether any role in the claims authorizes the given resource and action.
func (p *Provider) Enforce(claims types.Claims, resource, action string) bool {
	for _, role := range claims.Roles {
		resourcePerms, ok := p.perms[role]
		if !ok {
			continue
		}
		actions, ok := resourcePerms[resource]
		if !ok {
			continue
		}
		for _, a := range actions {
			if a == action || a == "*" {
				return true
			}
		}
	}
	return false
}

// Permissions lists all permissions for the user based on their roles.
// The result is deduplicated — if two roles grant the same action, it appears only once.
func (p *Provider) Permissions(claims types.Claims) []port.Permission {
	// resource to set of actions
	merged := make(map[string]map[string]struct{})

	for _, role := range claims.Roles {
		resourcePerms, ok := p.perms[role]
		if !ok {
			continue
		}
		for resource, actions := range resourcePerms {
			if merged[resource] == nil {
				merged[resource] = make(map[string]struct{})
			}
			for _, a := range actions {
				merged[resource][a] = struct{}{}
			}
		}
	}

	result := make([]port.Permission, 0, len(merged))
	for resource, actionSet := range merged {
		actions := make([]string, 0, len(actionSet))
		for a := range actionSet {
			actions = append(actions, a)
		}
		result = append(result, port.Permission{
			Resource: resource,
			Actions:  actions,
		})
	}
	return result
}

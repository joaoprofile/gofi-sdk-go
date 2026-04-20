package port

import "github.com/joaoprofile/gofi/iam/types"

// RBACPort abstracts the role-based authorization engine.
// May be replaced by ABAC, OPA, Cedar, or any other policy engine.
type RBACPort interface {
	// Enforce checks whether the claims authorize the given action on the resource.
	Enforce(claims types.Claims, resource, action string) bool

	// Permissions lists all permissions the user holds in the current context.
	Permissions(claims types.Claims) []Permission
}

// Permission represents a set of authorized actions on a resource.
type Permission struct {
	Resource string
	Actions  []string
}

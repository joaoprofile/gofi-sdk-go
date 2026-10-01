package main

import (
	"context"
	"errors"
	"slices"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/password"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
)

// The single tenant and module of this example. A multi-tenant app lists
// several and lets the user pick one after Authenticate.
const module = "app"

var tenant = types.Tenant{ID: "acme", Name: "Acme", Modules: []string{module}, Active: true}

// directory is an in-memory user store: it implements port.UserPort and
// port.TenantPort, the two ports iam calls to log someone in. In a real
// service they query your database; nothing else changes.
type directory struct {
	byEmail map[string]*types.User
	byID    map[string]*types.User
	roles   map[string][]string // userID -> roles in the tenant
}

func newDirectory() (*directory, error) {
	seed := []struct {
		id, email, password string
		roles               []string
	}{
		{"u-admin", "admin@example.com", "admin123", []string{"admin"}},
		{"u-viewer", "viewer@example.com", "viewer123", []string{"viewer"}},
	}

	d := &directory{byEmail: map[string]*types.User{}, byID: map[string]*types.User{}, roles: map[string][]string{}}
	for _, s := range seed {
		// Only the hash is kept. Argon2id with password.DefaultParams matches
		// the dummy check iam runs for unknown emails, so both cases take the
		// same time (with other params, set SecurityConfig.DummyPasswordHash).
		hash, err := password.Hash(s.password)
		if err != nil {
			return nil, err
		}
		u := &types.User{ID: s.id, Email: s.email, PasswordHash: hash, Active: true}
		d.byEmail[u.Email], d.byID[u.ID], d.roles[u.ID] = u, u, s.roles
	}
	return d, nil
}

// --- port.UserPort ---

func (d *directory) FindByID(_ context.Context, userID string) (*types.User, error) {
	if u, ok := d.byID[userID]; ok {
		return u, nil
	}
	return nil, core.ErrInvalidCredentials
}

func (d *directory) FindByEmail(_ context.Context, email string) (*types.User, error) {
	if u, ok := d.byEmail[email]; ok {
		return u, nil
	}
	return nil, core.ErrInvalidCredentials
}

func (d *directory) ValidatePassword(ctx context.Context, userID, pw string) error {
	u, err := d.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	return password.Verify(u.PasswordHash, pw)
}

func (d *directory) FindOrCreateByExternalIdentity(context.Context, types.ExternalIdentity) (*types.User, error) {
	return nil, errors.New("social login is not enabled in this example")
}

// --- port.TenantPort ---

func (d *directory) ListUserTenants(_ context.Context, userID string) ([]types.TenantAccess, error) {
	roles, ok := d.roles[userID]
	if !ok {
		return nil, nil
	}
	return []types.TenantAccess{{Tenant: tenant, Modules: tenant.Modules, Roles: roles}}, nil
}

// AssertAccess is checked at login and can be re-checked per request
// (iam/middleware.TenantMiddleware), so removing access takes effect at once.
func (d *directory) AssertAccess(_ context.Context, userID, tenantID, mod string) error {
	u, ok := d.byID[userID]
	if !ok || !u.Active || tenantID != tenant.ID || !slices.Contains(tenant.Modules, mod) {
		return core.ErrTenantAccessDenied
	}
	return nil
}

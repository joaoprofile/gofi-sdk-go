package port

import (
	"context"

	"github.com/gofi-labs/gofi-sdk-go/iam/types"
)

// AuthPort orchestrates the local authentication flow (email and password credentials).
// The developer may replace the entire implementation with an external provider
// such as Keycloak or Cognito without modifying the core.
type AuthPort interface {
	// Authenticate validates credentials and returns the available tenants.
	// Does not issue any token — the user does not have a session yet.
	Authenticate(ctx context.Context, input AuthInput) (*AuthResult, error)

	// SelectTenant validates access to the tenant and module, issues tokens, and creates the session.
	// The returned Session contains the raw RefreshToken (for the cookie) and the AccessToken.
	SelectTenant(ctx context.Context, input SelectTenantInput) (*types.Session, error)

	// Logout completely invalidates the session — a real logout independent of token expiry.
	Logout(ctx context.Context, sessionID string) error

	// RefreshToken renews the access token with mandatory refresh token rotation.
	// The current session is revoked and a new one is created.
	RefreshToken(ctx context.Context, refreshToken string) (*types.Session, error)

	// ValidateToken validates the signature and expiry of the access token and verifies
	// that the corresponding session is still active in the SessionPort.
	ValidateToken(ctx context.Context, accessToken string) (*types.Claims, error)
}

// AuthInput holds credentials for local authentication.
type AuthInput struct {
	Email    string
	Password string

	// IPAddress of the client, when known; keys the per-IP LoginThrottler limit.
	IPAddress string

	// Extra carries provider-specific input that does not fit Email/Password,
	// e.g. an external token already issued by a third party. Forwarded
	// unchanged to the custom AuthPort implementation; the built-in one ignores it.
	Extra map[string]string
}

// AuthResult is the result of the Authenticate step.
// Does not contain tokens — the user must select a tenant to obtain a session.
type AuthResult struct {
	UserID  string
	Tenants []types.TenantAccess

	// Ticket proves this authentication; pass it back in SelectTenantInput.Ticket.
	// Valid for 5 minutes and only for UserID.
	Ticket string

	// Extra carries handoff data that the custom AuthPort needs between
	// Authenticate and SelectTenant (e.g. an external session token). The
	// caller re-injects it into SelectTenantInput.SessionExtra (secrets) or
	// ClaimsExtra (public claims only).
	Extra map[string]string
}

// SelectTenantInput holds the parameters for tenant selection after authentication.
type SelectTenantInput struct {
	UserID   string
	TenantID string
	Module   string

	// Ticket from AuthResult/IDPCallbackResult, bound to UserID and the login
	// flow; required unless SecurityConfig.InsecureSkipTenantTicket is set.
	Ticket string

	// Audit and security context.
	IPAddress string
	UserAgent string
	DeviceID  string

	// ClaimsExtra goes into the access token ("ext" claim), readable by any
	// bearer: public labels only (e.g. a domain role), never secrets or tokens.
	ClaimsExtra map[string]string

	// SessionExtra stays server-side, persisted with the session and never put
	// in a token: the place for external provider tokens and other secrets.
	SessionExtra map[string]string
}

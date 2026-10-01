package config

import (
	"context"
	"net/http"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
)

// Config is the full IAMService configuration with complete control over providers.
// At minimum Token, Session, and RBAC are required.
// Auth may be nil to use the built-in implementation (requires User and Tenant).
// User and Tenant may be nil if a custom AuthPort encapsulates them.
type Config struct {
	// Domain ports.
	Auth    port.AuthPort    // nil uses the built-in implementation (requires User, Tenant, Token, Session)
	User    port.UserPort    // may be nil if Auth encapsulates UserPort
	Tenant  port.TenantPort  // may be nil if Auth encapsulates TenantPort
	Token   port.TokenPort   // required when Auth is nil (built-in)
	Session port.SessionPort // must never be nil
	RBAC    port.RBACPort    // required

	// Registered social IDPs. Key is the provider name ("google", "github", etc.).
	IDPs map[string]port.IDPAuthPort

	// LoginThrottler limits password guessing in the built-in login; nil disables it.
	LoginThrottler port.LoginThrottler
	// TicketStore makes tenant tickets single-use; nil keeps them reusable
	// until they expire (5 minutes).
	TicketStore port.TicketStore

	// Security settings.
	Security SecurityConfig

	// Observability callback invoked for every authentication and authorization event.
	// Implementations must never include passwords, raw tokens, or refresh tokens.
	OnEvent func(ctx context.Context, event types.IAMEvent)
}

// SecurityConfig defines the security parameters of the IAMService.
// Default values are production-safe.
type SecurityConfig struct {
	AccessTokenTTL  time.Duration // default: 15 min, enforced maximum: 60 min
	RefreshTokenTTL time.Duration // default: 7 days, enforced maximum: 90 days
	Issuer          string        // default: "gofi/iam"
	VerifyIssuer    bool          // reject tokens whose iss differs from Issuer; default: true
	// InsecureSkipIssuerCheck keeps VerifyIssuer off, accepting tokens of any
	// issuer that share the secret. Only for migrating legacy tokens.
	InsecureSkipIssuerCheck bool
	Audience                string // optional: issued as aud and required on validation; pin it per service

	// SessionMaxLifetime caps a session from its original login across all
	// refresh rotations; the user must log in again after it.
	// Default: 30 days (never below RefreshTokenTTL), enforced maximum: 90 days.
	SessionMaxLifetime time.Duration

	// TenantTicketSecret (≥32 bytes) signs the Authenticate→SelectTenant ticket,
	// which SelectTenant always requires. NewDefault derives it from JWTSecret
	// when empty; New fails without it when login is enabled.
	TenantTicketSecret []byte
	// InsecureSkipTenantTicket lets SelectTenant open a session for any
	// caller-supplied UserID without a ticket. Only for callers that already
	// authenticated the user server-side; never reachable from a public endpoint.
	InsecureSkipTenantTicket bool
	ClockSkew                time.Duration // optional: leeway for token time claims, at most 5 min

	// DummyPasswordHash is verified when the login email is unknown, so both
	// cases take the same time. Use a hash made with the same algorithm and
	// cost as the stored ones; default: Argon2id with password.DefaultParams.
	DummyPasswordHash string

	// Refresh token cookie settings, used by the middleware cookie helpers.
	// Cookies are HttpOnly and Secure unless CookieInsecure is set.
	CookieName     string        // default: "iam_rt"
	CookiePath     string        // default: "/auth/refresh"
	CookieInsecure bool          // drops Secure; only for local http development
	CookieSameSite http.SameSite // default: SameSiteStrictMode
	CookieDomain   string        // optional

	// Temporary cookie for OAuth2 state and PKCE verifier (always SameSite=Lax,
	// so it survives the IDP's top-level redirect back).
	IDPStateCookieName string        // default: "iam_idp_state"
	IDPStateTTL        time.Duration // default: 10 min
}

// DefaultConfig is the shortcut for quick setup with built-in providers.
// Selects Redis if RedisAddr is configured, otherwise uses in-memory with TTL.
type DefaultConfig struct {
	JWTSecret string // HS256, minimum 32 chars
	// Key rotation: tokens carry JWTKeyID as kid; tokens signed with the
	// previous secret stay valid until they expire.
	JWTKeyID          string
	JWTPreviousKeyID  string
	JWTPreviousSecret string

	// Session provider.
	RedisAddr     string // empty string selects in-memory with TTL
	RedisPassword string
	RedisTLS      bool

	// Login throttling (Redis when RedisAddr is set, in memory otherwise):
	// failures per email within the window before a lockout. 0 uses the
	// default (5); negative disables throttling. The per-IP limit is 4x.
	LoginMaxAttempts int
	LoginLockout     time.Duration // default: 15 min; also the counting window

	// IDPs (optional).
	IDPs []IDPConfig

	// Security settings (uses safe defaults when nil).
	Security *SecurityConfig

	// Observability callback.
	OnEvent func(ctx context.Context, event types.IAMEvent)

	// Application ports. User and Tenant enable login (Authenticate,
	// SelectTenant, RefreshToken and IDP callbacks); without them the service
	// only validates tokens and revokes sessions. RBAC backs IAMService.RBAC.
	User   port.UserPort
	Tenant port.TenantPort
	RBAC   port.RBACPort
}

// IDPConfig configures an external identity provider in DefaultConfig.
type IDPConfig struct {
	Provider     string // "google", "github", "microsoft", "oidc"
	ClientID     string
	ClientSecret string
	RedirectURI  string

	// Required for the generic oidc provider.
	IssuerURL string
	Scopes    []string
}

// DefaultSessionMaxLifetime is the default absolute session lifetime.
const DefaultSessionMaxLifetime = 30 * 24 * time.Hour

// ApplyDefaults fills in default values for SecurityConfig.
// Safe to call multiple times as it does not overwrite values already set.
func (s *SecurityConfig) ApplyDefaults() {
	if s.AccessTokenTTL == 0 {
		s.AccessTokenTTL = 15 * time.Minute
	}
	if s.RefreshTokenTTL == 0 {
		s.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if s.SessionMaxLifetime == 0 {
		s.SessionMaxLifetime = max(DefaultSessionMaxLifetime, s.RefreshTokenTTL)
	}
	if s.Issuer == "" {
		s.Issuer = "gofi/iam"
	}
	if !s.InsecureSkipIssuerCheck {
		s.VerifyIssuer = true
	}
	if s.CookieName == "" {
		s.CookieName = "iam_rt"
	}
	if s.CookiePath == "" {
		s.CookiePath = "/auth/refresh"
	}
	if s.CookieSameSite == 0 {
		s.CookieSameSite = http.SameSiteStrictMode
	}
	if s.IDPStateCookieName == "" {
		s.IDPStateCookieName = "iam_idp_state"
	}
	if s.IDPStateTTL == 0 {
		s.IDPStateTTL = 10 * time.Minute
	}
}

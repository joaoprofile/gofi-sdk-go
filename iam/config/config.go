package config

import (
	"context"
	"net/http"
	"time"

	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
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

	// Refresh token cookie settings.
	CookieName     string        // default: "iam_rt"
	CookiePath     string        // default: "/auth/refresh"
	CookieSecure   bool          // default: true
	CookieSameSite http.SameSite // default: SameSiteStrictMode
	CookieDomain   string        // optional

	// Temporary cookie for OAuth2 state and PKCE verifier.
	IDPStateCookieName string        // default: "iam_idp_state"
	IDPStateTTL        time.Duration // default: 10 min
}

// DefaultConfig is the shortcut for quick setup with built-in providers.
// Selects Redis if RedisAddr is configured, otherwise uses in-memory with TTL.
type DefaultConfig struct {
	JWTSecret string // HS256, minimum 32 chars

	// Session provider.
	RedisAddr     string // empty string selects in-memory with TTL
	RedisPassword string
	RedisTLS      bool

	// IDPs (optional).
	IDPs []IDPConfig

	// Security settings (uses safe defaults when nil).
	Security *SecurityConfig

	// Observability callback.
	OnEvent func(ctx context.Context, event types.IAMEvent)
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

// ApplyDefaults fills in default values for SecurityConfig.
// Safe to call multiple times as it does not overwrite values already set.
func (s *SecurityConfig) ApplyDefaults() {
	if s.AccessTokenTTL == 0 {
		s.AccessTokenTTL = 15 * time.Minute
	}
	if s.RefreshTokenTTL == 0 {
		s.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if s.Issuer == "" {
		s.Issuer = "gofi/iam"
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

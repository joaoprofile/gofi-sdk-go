// Package iam is an identity and access facade for the gofi SDK.
//
// Provides authentication (local and social), RBAC authorization, session management
// with real revocation, and multi-tenancy with zero infrastructure provider lock-in.
package iam

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"time"

	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/google"
	jwtprovider "github.com/joaoprofile/gofi-sdk-go/iam/provider/jwt"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/memory"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/microsoft"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/oidc"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/password"
	redisprovider "github.com/joaoprofile/gofi-sdk-go/iam/provider/redis"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

// Re-exports so callers do not need to import sub-packages in simple use cases.
type (
	Config         = iamconfig.Config
	DefaultConfig  = iamconfig.DefaultConfig
	SecurityConfig = iamconfig.SecurityConfig
	IDPConfig      = iamconfig.IDPConfig
)

// New builds an IAMService with full control over providers.
// Validates minimum security contracts before returning and returns an error if any are violated.
func New(cfg Config) (*core.IAMService, error) {
	cfg.Security.ApplyDefaults()

	if err := validate(cfg); err != nil {
		return nil, err
	}

	emit := cfg.OnEvent
	if emit == nil {
		emit = func(context.Context, types.IAMEvent) {}
	}

	authCfg := core.AuthConfigFromSecurity(cfg.Security)

	// Build the AuthPort: custom or built-in.
	auth := cfg.Auth
	if auth == nil {
		auth = core.NewLocalAuth(core.LocalAuthConfig{
			User:      cfg.User,
			Tenant:    cfg.Tenant,
			Token:     cfg.Token,
			Session:   cfg.Session,
			Cfg:       authCfg,
			Emit:      emit,
			Throttler: cfg.LoginThrottler,
			Tickets:   cfg.TicketStore,
		})
	}

	// Build the IDP services.
	idps := make(map[string]*core.IDPService)
	for name, p := range cfg.IDPs {
		idps[name] = core.NewIDPService(core.IDPServiceConfig{
			Provider: p,
			User:     cfg.User,
			Tenant:   cfg.Tenant,
			Token:    cfg.Token,
			Session:  cfg.Session,
			Cfg:      authCfg,
			Emit:     emit,
			Tickets:  cfg.TicketStore,
		})
	}

	return core.NewService(core.ServiceConfig{
		Auth:    auth,
		Session: cfg.Session,
		Tenant:  cfg.Tenant,
		RBAC:    cfg.RBAC,
		IDPs:    idps,
		OnEvent: emit,
	}), nil
}

// NewDefault builds an IAMService with built-in providers from DefaultConfig.
// Sessions, login throttling and single-use tenant tickets live in Redis when
// RedisAddr is configured (one shared client), in memory otherwise.
func NewDefault(cfg DefaultConfig) (*core.IAMService, error) {
	sec := cfg.Security
	if sec == nil {
		sec = &SecurityConfig{}
	}
	sec.ApplyDefaults()

	stores := defaultStores(cfg)

	// JWT token provider (see previousKeys for rotation).
	tokenProvider, err := jwtprovider.NewProvider(jwtprovider.Config{
		Algorithm:        jwtprovider.HS256,
		Secret:           []byte(cfg.JWTSecret),
		KeyID:            cfg.JWTKeyID,
		VerificationKeys: previousKeys(cfg),
		AccessTokenTTL:   sec.AccessTokenTTL,
		Issuer:           sec.Issuer,
		VerifyIssuer:     sec.VerifyIssuer,
		Audience:         sec.Audience,
		Leeway:           sec.ClockSkew,
	})
	if err != nil {
		return nil, fmt.Errorf("iam: failed to create JWT provider: %w", err)
	}

	// IDP providers.
	idpPorts := make(map[string]port.IDPAuthPort)
	for _, idpCfg := range cfg.IDPs {
		p, err := buildIDPProvider(idpCfg)
		if err != nil {
			return nil, fmt.Errorf("iam: failed to build IDP provider %q: %w", idpCfg.Provider, err)
		}
		idpPorts[idpCfg.Provider] = p
	}

	return New(Config{
		User:           cfg.User,
		Tenant:         cfg.Tenant,
		RBAC:           cfg.RBAC,
		Token:          tokenProvider,
		Session:        stores.session,
		LoginThrottler: stores.throttler,
		TicketStore:    stores.tickets,
		IDPs:           idpPorts,
		Security:       withTicketSecret(*sec, cfg.JWTSecret),
		OnEvent:        cfg.OnEvent,
	})
}

// stores are the stateful ports NewDefault builds: Redis (one shared client)
// when RedisAddr is set, in memory otherwise.
type stores struct {
	session   port.SessionPort
	throttler port.LoginThrottler
	tickets   port.TicketStore
}

func defaultStores(cfg DefaultConfig) stores {
	lockout := cfg.LoginLockout
	if cfg.RedisAddr == "" {
		s := stores{session: memory.NewProvider(), tickets: memory.NewTicketStore()}
		if cfg.LoginMaxAttempts >= 0 {
			s.throttler = memory.NewLoginThrottler(memory.ThrottleConfig{
				MaxAttempts: cfg.LoginMaxAttempts, Window: lockout, Lockout: lockout,
			})
		}
		return s
	}
	rp := redisprovider.NewProvider(redisprovider.Config{
		Addr:       cfg.RedisAddr,
		Password:   cfg.RedisPassword,
		TLSEnabled: cfg.RedisTLS,
	})
	s := stores{session: rp, tickets: redisprovider.NewTicketStore(rp.Client(), "")}
	if cfg.LoginMaxAttempts >= 0 {
		s.throttler = redisprovider.NewLoginThrottler(rp.Client(), redisprovider.ThrottleConfig{
			MaxAttempts: cfg.LoginMaxAttempts, Window: lockout, Lockout: lockout,
		})
	}
	return s
}

// validate checks minimum security contracts.
// The SDK refuses to initialize if any constraint is violated.
func validate(cfg Config) error {
	if cfg.Session == nil {
		return core.ErrSessionPortRequired
	}
	if cfg.Security.AccessTokenTTL > 60*time.Minute {
		return core.ErrAccessTokenTTLExceeded
	}
	if cfg.Security.RefreshTokenTTL > 90*24*time.Hour {
		return core.ErrRefreshTokenTTLExceeded
	}
	if cfg.Security.SessionMaxLifetime > 90*24*time.Hour {
		return core.ErrSessionMaxLifetimeExceeded
	}
	if cfg.Security.ClockSkew > core.MaxClockSkew {
		return core.ErrClockSkewExceeded
	}
	if h := cfg.Security.DummyPasswordHash; h != "" && !password.Supported(h) {
		return fmt.Errorf("iam: DummyPasswordHash is not an Argon2id or bcrypt hash")
	}
	n := len(cfg.Security.TenantTicketSecret)
	if n > 0 && n < 32 {
		return core.ErrTenantTicketSecretTooShort
	}
	// Built-in SelectTenant (local or IDP) trusts the caller's UserID only with a ticket.
	loginEnabled := (cfg.Auth == nil && cfg.Tenant != nil) || len(cfg.IDPs) > 0
	if loginEnabled && n == 0 && !cfg.Security.InsecureSkipTenantTicket {
		return core.ErrTenantTicketSecretRequired
	}
	return nil
}

// buildIDPProvider builds the IDPAuthPort corresponding to the given IDPConfig.
func buildIDPProvider(cfg IDPConfig) (port.IDPAuthPort, error) {
	switch cfg.Provider {
	case "google":
		return google.New(google.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURI:  cfg.RedirectURI,
		}), nil
	case "microsoft":
		return microsoft.New(microsoft.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURI:  cfg.RedirectURI,
		}), nil
	case "oidc":
		if cfg.IssuerURL == "" {
			return nil, fmt.Errorf("IssuerURL is required for oidc provider")
		}
		return oidc.New("oidc", oidc.Config{
			IssuerURL:    cfg.IssuerURL,
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURI:  cfg.RedirectURI,
			ExtraScopes:  cfg.Scopes,
		}), nil
	default:
		return nil, fmt.Errorf("unknown IDP provider: %s", cfg.Provider)
	}
}

// withTicketSecret derives the ticket key from the JWT secret without mutating the caller's config.
func withTicketSecret(sec SecurityConfig, jwtSecret string) SecurityConfig {
	if len(sec.TenantTicketSecret) == 0 {
		sec.TenantTicketSecret = deriveKey(jwtSecret, "gofi/iam tenant-ticket")
	}
	return sec
}

func deriveKey(secret, label string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(label))
	return m.Sum(nil)
}

// previousKeys maps the previous secret to its kid for rotation.
func previousKeys(cfg DefaultConfig) map[string]any {
	if cfg.JWTPreviousSecret == "" {
		return nil
	}
	return map[string]any{cfg.JWTPreviousKeyID: []byte(cfg.JWTPreviousSecret)}
}

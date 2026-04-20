// Package iam is an identity and access facade for the gofi SDK.
//
// Provides authentication (local and social), RBAC authorization, session management
// with real revocation, and multi-tenancy with zero infrastructure provider lock-in.
package iam

import (
	"context"
	"fmt"
	"time"

	iamconfig "github.com/joaoprofile/gofi/iam/config"
	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/provider/google"
	jwtprovider "github.com/joaoprofile/gofi/iam/provider/jwt"
	"github.com/joaoprofile/gofi/iam/provider/memory"
	"github.com/joaoprofile/gofi/iam/provider/microsoft"
	"github.com/joaoprofile/gofi/iam/provider/oidc"
	redisprovider "github.com/joaoprofile/gofi/iam/provider/redis"
	"github.com/joaoprofile/gofi/iam/types"
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
			User:    cfg.User,
			Tenant:  cfg.Tenant,
			Token:   cfg.Token,
			Session: cfg.Session,
			Cfg:     authCfg,
			Emit:    emit,
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
// Selects Redis if RedisAddr is configured, otherwise uses in-memory with TTL.
func NewDefault(cfg DefaultConfig) (*core.IAMService, error) {
	sec := cfg.Security
	if sec == nil {
		sec = &SecurityConfig{}
	}
	sec.ApplyDefaults()

	// Session provider.
	var sessionProvider port.SessionPort
	if cfg.RedisAddr != "" {
		sessionProvider = redisprovider.NewProvider(redisprovider.Config{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		})
	} else {
		sessionProvider = memory.NewProvider()
	}

	// JWT token provider.
	tokenProvider, err := jwtprovider.NewProvider(jwtprovider.Config{
		Algorithm:      jwtprovider.HS256,
		Secret:         []byte(cfg.JWTSecret),
		AccessTokenTTL: sec.AccessTokenTTL,
		Issuer:         sec.Issuer,
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
		Token:    tokenProvider,
		Session:  sessionProvider,
		IDPs:     idpPorts,
		Security: *sec,
		OnEvent:  cfg.OnEvent,
	})
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

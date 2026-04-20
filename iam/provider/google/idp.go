// Package google implements IDPAuthPort for Google OAuth2/OIDC.
// Uses PKCE (S256), automatic discovery, and id_token validation via JWKS.
package google

import (
	"context"
	"net/http"
	"time"

	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/provider/oidc"
)

const (
	issuerURL    = "https://accounts.google.com"
	providerName = "google"
)

// Config configures the Google provider.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string

	// HostedDomain restricts login to users from a specific Google Workspace domain.
	// Leave empty to accept any Google account.
	HostedDomain string

	// HTTPClient allows injecting a custom client useful for tests.
	HTTPClient *http.Client
}

// Provider implements port.IDPAuthPort for Google.
type Provider struct {
	inner *oidc.Provider
}

// New creates a Google OIDC provider.
func New(cfg Config) *Provider {
	var extraScopes []string
	if cfg.HostedDomain != "" {
		extraScopes = append(extraScopes, "hd")
	}

	inner := oidc.New(providerName, oidc.Config{
		IssuerURL:    issuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
		ExtraScopes:  extraScopes,
		JWKSCacheTTL: time.Hour,
		HTTPClient:   cfg.HTTPClient,
	})

	return &Provider{inner: inner}
}

func (p *Provider) ProviderName() string { return providerName }

func (p *Provider) AuthorizationURL(ctx context.Context, input port.IDPAuthInput) (*port.IDPAuthURL, error) {
	return p.inner.AuthorizationURL(ctx, input)
}

func (p *Provider) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	return p.inner.HandleCallback(ctx, input)
}

// Package microsoft implements IDPAuthPort for Microsoft Identity Platform (Entra ID).
// Supports single-tenant, multi-tenant, and personal Microsoft accounts.
package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/provider/oidc"
)

const providerName = "microsoft"

// Config configures the Microsoft Entra ID provider.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string

	// TenantID defines the authentication scope.
	// A specific Azure AD tenant ID restricts login to that tenant only.
	// "organizations" accepts any Azure AD tenant.
	// "consumers" accepts only personal Microsoft accounts.
	// "common" accepts both and is the default.
	TenantID string

	// HTTPClient allows injecting a custom client useful for tests.
	HTTPClient *http.Client
}

// Provider implements port.IDPAuthPort for Microsoft Entra.
type Provider struct {
	inner *oidc.Provider
}

// New creates a Microsoft OIDC provider.
func New(cfg Config) *Provider {
	if cfg.TenantID == "" {
		cfg.TenantID = "common"
	}

	issuerURL := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)

	inner := oidc.New(providerName, oidc.Config{
		IssuerURL:    issuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
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

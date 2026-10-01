// Package google implements IDPAuthPort for Google OAuth2/OIDC.
// Uses PKCE (S256), automatic discovery, and id_token validation via JWKS.
package google

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/oidc"
)

const (
	issuerURL    = "https://accounts.google.com"
	providerName = "google"
)

// ErrHostedDomainMismatch is returned when the id_token hd claim differs from Config.HostedDomain.
var ErrHostedDomainMismatch = errors.New("iam/google: hosted domain mismatch")

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
	oc := oidc.Config{
		IssuerURL:    issuerURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
		JWKSCacheTTL: time.Hour,
		HTTPClient:   cfg.HTTPClient,
	}
	if cfg.HostedDomain != "" {
		// hd in the URL is only a UI hint; the claim check is what enforces the domain.
		oc.AuthParams = map[string]string{"hd": cfg.HostedDomain}
		oc.ValidateClaims = hostedDomainValidator(cfg.HostedDomain)
	}

	return &Provider{inner: oidc.New(providerName, oc)}
}

func hostedDomainValidator(domain string) func(map[string]any) error {
	return func(claims map[string]any) error {
		if hd, _ := claims["hd"].(string); !strings.EqualFold(hd, domain) {
			return ErrHostedDomainMismatch
		}
		return nil
	}
}

func (p *Provider) ProviderName() string { return providerName }

func (p *Provider) AuthorizationURL(ctx context.Context, input port.IDPAuthInput) (*port.IDPAuthURL, error) {
	return p.inner.AuthorizationURL(ctx, input)
}

func (p *Provider) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	return p.inner.HandleCallback(ctx, input)
}

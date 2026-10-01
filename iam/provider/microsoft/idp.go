// Package microsoft implements IDPAuthPort for Microsoft Identity Platform (Entra ID).
// Supports single-tenant, multi-tenant, and personal Microsoft accounts.
package microsoft

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/oidc"
)

const (
	providerName = "microsoft"
	issuerHost   = "https://login.microsoftonline.com/"

	// consumersTenantID is the fixed tid of personal Microsoft accounts.
	consumersTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"
)

var (
	// ErrIssuerMismatch is returned when iss is not the Entra issuer of the token's tid.
	ErrIssuerMismatch = errors.New("iam/microsoft: issuer does not match token tenant")
	// ErrMissingObjectID is returned when the id_token lacks tid or oid.
	ErrMissingObjectID = errors.New("iam/microsoft: id_token without tid/oid (profile scope required)")
	// ErrTenantNotAllowed is returned when the token tenant is outside the configured scope.
	ErrTenantNotAllowed = errors.New("iam/microsoft: tenant not allowed")
)

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

	// AllowedTenants optionally restricts logins to these tenant IDs (tid claim).
	AllowedTenants []string

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

	issuerURL := fmt.Sprintf("%s%s/v2.0", issuerHost, cfg.TenantID)

	inner := oidc.New(providerName, oidc.Config{
		IssuerURL:      issuerURL,
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		RedirectURI:    cfg.RedirectURI,
		JWKSCacheTTL:   time.Hour,
		HTTPClient:     cfg.HTTPClient,
		ValidateIssuer: issuerValidator(cfg.TenantID, cfg.AllowedTenants),
	})

	return &Provider{inner: inner}
}

func (p *Provider) ProviderName() string { return providerName }

func (p *Provider) AuthorizationURL(ctx context.Context, input port.IDPAuthInput) (*port.IDPAuthURL, error) {
	return p.inner.AuthorizationURL(ctx, input)
}

// HandleCallback identifies the user by tenant and object ID. Entra's sub is
// pairwise per application, so it changes with the app registration; tid+oid
// is stable across apps and matches Microsoft Graph.
func (p *Provider) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	res, err := p.inner.HandleCallback(ctx, input)
	if err != nil {
		return nil, err
	}
	id, err := externalID(res.IDPUser.RawClaims)
	if err != nil {
		return nil, err
	}
	res.IDPUser.ExternalID = id
	// Entra's email claim is admin-editable and unverified: never trust it
	// for account linking.
	res.IDPUser.EmailVerified = false
	return res, nil
}

// externalID returns "<tid>:<oid>"; tokens without oid are rejected rather
// than falling back to the unstable sub.
func externalID(claims map[string]any) (string, error) {
	tid, _ := claims["tid"].(string)
	oid, _ := claims["oid"].(string)
	if tid == "" || oid == "" {
		return "", ErrMissingObjectID
	}
	return strings.ToLower(tid) + ":" + strings.ToLower(oid), nil
}

// issuerValidator binds iss to the token's own tid, then applies the tenant scope.
func issuerValidator(tenantID string, allowed []string) func(map[string]any) error {
	return func(claims map[string]any) error {
		tid, _ := claims["tid"].(string)
		iss, _ := claims["iss"].(string)
		if tid == "" || iss != issuerHost+tid+"/v2.0" {
			return ErrIssuerMismatch
		}
		if !tenantInScope(tenantID, tid) || !tenantAllowed(allowed, tid) {
			return ErrTenantNotAllowed
		}
		return nil
	}
}

// tenantInScope reports whether tid fits the configured tenant segment
// (common, organizations, consumers or a specific tenant).
func tenantInScope(tenantID, tid string) bool {
	switch strings.ToLower(tenantID) {
	case "common":
		return true
	case "organizations":
		return tid != consumersTenantID
	case "consumers":
		return tid == consumersTenantID
	default:
		// A domain-name tenant cannot be compared to tid; rely on AllowedTenants.
		return uuid.Validate(tenantID) != nil || strings.EqualFold(tid, tenantID)
	}
}

// tenantAllowed applies AllowedTenants; an empty list allows any tenant.
func tenantAllowed(allowed []string, tid string) bool {
	return len(allowed) == 0 || slices.ContainsFunc(allowed, func(a string) bool { return strings.EqualFold(a, tid) })
}

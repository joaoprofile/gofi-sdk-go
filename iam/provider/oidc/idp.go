// Package oidc implements a generic IDPAuthPort for any OIDC-compliant provider.
// Supports PKCE (RFC 7636 S256), id_token validation via JWKS, and automatic discovery.
package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/internal/flight"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/jwks"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

// Config configures the generic OIDC provider.
type Config struct {
	// IssuerURL is the base URL of the provider.
	// The discovery document is fetched from IssuerURL + "/.well-known/openid-configuration".
	IssuerURL string

	ClientID     string
	ClientSecret string
	// RedirectURI is used when the flow input has none; a different one is rejected.
	RedirectURI string

	// Scopes beyond the minimum set (openid email profile).
	ExtraScopes []string

	// AuthParams are extra authorization URL parameters; standard ones cannot be overridden.
	AuthParams map[string]string

	// ValidateClaims runs after signature, issuer and audience checks of the id_token.
	ValidateClaims func(claims map[string]any) error

	// ValidateIssuer replaces the exact IssuerURL match, for multi-tenant issuers.
	ValidateIssuer func(claims map[string]any) error

	// JWKSCacheTTL defines how long JWKS keys are cached. Default: 1 hour.
	JWKSCacheTTL time.Duration

	// ClockSkew is the leeway for the id_token exp/iat/nbf. Default: 1 minute;
	// capped at 5 minutes.
	ClockSkew time.Duration

	// HTTPClient allows injecting a custom client useful for tests.
	HTTPClient *http.Client
}

// defaultClockSkew is the id_token leeway when Config.ClockSkew is zero.
const defaultClockSkew = time.Minute

// Provider implements port.IDPAuthPort for any OIDC-compliant provider.
type Provider struct {
	cfg           Config
	name          string
	client        *http.Client
	discMu        sync.RWMutex
	discovery     *discoveryDoc
	discFetchedAt time.Time
	discFlight    flight.Group
	jwks          *jwks.Cache
}

// New creates a generic OIDC provider with the given identifier name.
func New(name string, cfg Config) *Provider {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.JWKSCacheTTL == 0 {
		cfg.JWKSCacheTTL = time.Hour
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultClockSkew
	}
	cfg.ClockSkew = min(cfg.ClockSkew, core.MaxClockSkew)
	return &Provider{
		cfg:    cfg,
		name:   name,
		client: cfg.HTTPClient,
		jwks:   jwks.NewCache(cfg.JWKSCacheTTL),
	}
}

func (p *Provider) ProviderName() string { return p.name }

// AuthorizationURL generates the OIDC authorization URL with PKCE and state.
func (p *Provider) AuthorizationURL(ctx context.Context, input port.IDPAuthInput) (*port.IDPAuthURL, error) {
	disc, err := p.getDiscovery(ctx)
	if err != nil {
		return nil, err
	}

	redirectURI, err := p.redirectURI(input.RedirectURI)
	if err != nil {
		return nil, err
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	scopes := append([]string{"openid", "email", "profile"}, input.Scopes...)
	scopes = append(scopes, p.cfg.ExtraScopes...)
	scopes = unique(scopes)

	params := url.Values{}
	for k, v := range p.cfg.AuthParams {
		params.Set(k, v)
	}
	params.Set("response_type", "code")
	params.Set("client_id", p.cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(scopes, " "))
	params.Set("state", input.State)
	nonce := input.Nonce
	if nonce == "" {
		nonce = core.NonceForState(input.State)
	}
	params.Set("nonce", nonce)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")

	authURL := disc.AuthorizationEndpoint + "?" + params.Encode()

	return &port.IDPAuthURL{
		URL:           authURL,
		State:         input.State,
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		Nonce:         nonce,
	}, nil
}

// HandleCallback processes the OIDC callback: validates state, exchanges the code, and validates the id_token.
func (p *Provider) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	// Empty values compare equal and would derive a public nonce: login CSRF.
	if input.State == "" || input.ExpectedState == "" || input.CodeVerifier == "" ||
		subtle.ConstantTimeCompare([]byte(input.State), []byte(input.ExpectedState)) == 0 {
		return nil, core.ErrInvalidIDPState
	}
	redirectURI, err := p.redirectURI(input.RedirectURI)
	if err != nil {
		return nil, err
	}
	input.RedirectURI = redirectURI

	disc, err := p.getDiscovery(ctx)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: discovery failed: %w", err)
	}

	tokens, err := p.exchangeCode(ctx, disc.TokenEndpoint, input)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: token exchange failed: %w", err)
	}

	expectedNonce := input.ExpectedNonce
	if expectedNonce == "" {
		expectedNonce = core.NonceForState(input.ExpectedState)
	}
	idpUser, err := p.validateIDToken(ctx, disc, tokens.IDToken, expectedNonce)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: id_token validation failed: %w", err)
	}

	return &port.IDPCallbackResult{
		IDPUser:   *idpUser,
		IsNewUser: false, // determined by UserPort.FindOrCreateByExternalIdentity
	}, nil
}

type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// discoveryTTL bounds how long a discovery document is reused before refetching.
const discoveryTTL = 24 * time.Hour

// redirectURI defaults to Config.RedirectURI and rejects any other value when it is set.
func (p *Provider) redirectURI(in string) (string, error) {
	switch {
	case p.cfg.RedirectURI == "" || in == p.cfg.RedirectURI:
		return in, nil
	case in == "":
		return p.cfg.RedirectURI, nil
	default:
		return "", ErrRedirectURIMismatch
	}
}

// getDiscovery returns the cached document, refetching after discoveryTTL; on a
// failed refresh the previous document is served so an IdP blip does not break login.
// Concurrent refetches share one request and no lock is held during it.
func (p *Provider) getDiscovery(ctx context.Context) (*discoveryDoc, error) {
	p.discMu.RLock()
	doc, fresh := p.discovery, time.Since(p.discFetchedAt) < discoveryTTL
	p.discMu.RUnlock()
	if doc != nil && fresh {
		return doc, nil
	}
	err := p.discFlight.Do(ctx, func(fctx context.Context) error {
		d, err := p.fetchDiscovery(fctx)
		if err != nil {
			return err
		}
		p.discMu.Lock()
		p.discovery, p.discFetchedAt = d, time.Now()
		p.discMu.Unlock()
		return nil
	})
	p.discMu.RLock()
	doc = p.discovery
	p.discMu.RUnlock()
	if doc != nil {
		return doc, nil
	}
	return nil, err
}

func (p *Provider) fetchDiscovery(ctx context.Context) (*discoveryDoc, error) {
	discURL := strings.TrimRight(p.cfg.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iam/oidc: discovery returned status %d", resp.StatusCode)
	}
	var doc discoveryDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("iam/oidc: failed to decode discovery: %w", err)
	}
	// Multi-tenant issuers (e.g. Entra {tenantid}) are checked per token by ValidateIssuer.
	if p.cfg.ValidateIssuer == nil && strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(p.cfg.IssuerURL, "/") {
		return nil, fmt.Errorf("iam/oidc: discovery issuer %q does not match %q", doc.Issuer, p.cfg.IssuerURL)
	}
	return &doc, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (p *Provider) exchangeCode(ctx context.Context, tokenEndpoint string, input port.IDPCallbackInput) (*tokenResponse, error) {
	// The request carries the client secret and the code.
	if err := jwks.RequireHTTPS(tokenEndpoint); err != nil {
		return nil, err
	}
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", input.Code)
	body.Set("redirect_uri", input.RedirectURI)
	body.Set("client_id", p.cfg.ClientID)
	body.Set("client_secret", p.cfg.ClientSecret)
	body.Set("code_verifier", input.CodeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, newTokenEndpointError(resp)
	}

	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func (p *Provider) validateIDToken(ctx context.Context, disc *discoveryDoc, idToken, expectedNonce string) (*types.IDPUser, error) {
	// Parse without verifying signature to extract the kid from the header.
	unverified, _, err := gojwt.NewParser().ParseUnverified(idToken, gojwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse id_token header: %w", err)
	}

	kid, _ := unverified.Header["kid"].(string)
	alg, _ := unverified.Header["alg"].(string)

	key, err := p.jwks.GetKey(ctx, p.client, disc.JWKSURI, kid)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWKS key: %w", err)
	}

	signingMethod := signingMethodFor(alg)
	claims := gojwt.MapClaims{}
	parsed, err := gojwt.ParseWithClaims(idToken, claims, func(t *gojwt.Token) (any, error) {
		if t.Method.Alg() != signingMethod.Alg() {
			return nil, fmt.Errorf("unexpected alg: %s", t.Method.Alg())
		}
		return key, nil
	}, p.idTokenParserOptions()...)
	if err != nil {
		return nil, fmt.Errorf("id_token validation failed: %w", err)
	}

	if !parsed.Valid {
		return nil, fmt.Errorf("id_token is invalid")
	}

	mc, _ := parsed.Claims.(gojwt.MapClaims)
	return p.checkIDTokenClaims(mc, expectedNonce)
}

// signingMethodFor maps the header alg; anything else is checked as RS256 and fails.
func signingMethodFor(alg string) gojwt.SigningMethod {
	if alg == "ES256" {
		return gojwt.SigningMethodES256
	}
	return gojwt.SigningMethodRS256
}

// idTokenParserOptions require exp, bound iat and apply the capped leeway.
func (p *Provider) idTokenParserOptions() []gojwt.ParserOption {
	opts := []gojwt.ParserOption{
		gojwt.WithAudience(p.cfg.ClientID),
		gojwt.WithExpirationRequired(),
		gojwt.WithIssuedAt(), // iat must not be in the future beyond the leeway
		gojwt.WithLeeway(p.cfg.ClockSkew),
	}
	if p.cfg.ValidateIssuer == nil {
		opts = append(opts, gojwt.WithIssuer(p.cfg.IssuerURL))
	}
	return opts
}

// checkIDTokenClaims runs the issuer, nonce and custom checks on verified claims.
func (p *Provider) checkIDTokenClaims(mc gojwt.MapClaims, expectedNonce string) (*types.IDPUser, error) {
	if p.cfg.ValidateIssuer != nil {
		if err := p.cfg.ValidateIssuer(mc); err != nil {
			return nil, err
		}
	}
	if nonce, _ := mc["nonce"].(string); expectedNonce == "" ||
		subtle.ConstantTimeCompare([]byte(nonce), []byte(expectedNonce)) == 0 {
		return nil, core.ErrInvalidIDPNonce
	}
	if p.cfg.ValidateClaims != nil {
		if err := p.cfg.ValidateClaims(mc); err != nil {
			return nil, err
		}
	}
	return mapClaims(mc, p.name), nil
}

func mapClaims(mc gojwt.MapClaims, provider string) *types.IDPUser {
	get := func(k string) string {
		v, _ := mc[k].(string)
		return v
	}
	getBool := func(k string) bool {
		v, _ := mc[k].(bool)
		return v
	}

	raw := make(map[string]any, len(mc))
	maps.Copy(raw, mc)

	return &types.IDPUser{
		ExternalID:    get("sub"),
		Provider:      provider,
		Email:         get("email"),
		EmailVerified: getBool("email_verified"),
		Name:          get("name"),
		PictureURL:    get("picture"),
		RawClaims:     raw,
	}
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 64)
	if _, err = cryptoRandRead(b); err != nil {
		return "", "", fmt.Errorf("iam/oidc: failed to generate PKCE: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// unique removes duplicates while preserving order.
func unique(ss []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

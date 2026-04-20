// Package oidc implements a generic IDPAuthPort for any OIDC-compliant provider.
// Supports PKCE (RFC 7636 S256), id_token validation via JWKS, and automatic discovery.
package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
)

// Config configures the generic OIDC provider.
type Config struct {
	// IssuerURL is the base URL of the provider.
	// The discovery document is fetched from IssuerURL + "/.well-known/openid-configuration".
	IssuerURL string

	ClientID     string
	ClientSecret string
	RedirectURI  string

	// Scopes beyond the minimum set (openid email profile).
	ExtraScopes []string

	// JWKSCacheTTL defines how long JWKS keys are cached. Default: 1 hour.
	JWKSCacheTTL time.Duration

	// HTTPClient allows injecting a custom client useful for tests.
	HTTPClient *http.Client
}

// Provider implements port.IDPAuthPort for any OIDC-compliant provider.
type Provider struct {
	cfg       Config
	name      string
	client    *http.Client
	discovery *discoveryDoc
	discOnce  sync.Once
	jwks      *jwksCache
}

// New creates a generic OIDC provider with the given identifier name.
func New(name string, cfg Config) *Provider {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.JWKSCacheTTL == 0 {
		cfg.JWKSCacheTTL = time.Hour
	}
	return &Provider{
		cfg:    cfg,
		name:   name,
		client: cfg.HTTPClient,
		jwks:   &jwksCache{ttl: cfg.JWKSCacheTTL},
	}
}

func (p *Provider) ProviderName() string { return p.name }

// AuthorizationURL generates the OIDC authorization URL with PKCE and state.
func (p *Provider) AuthorizationURL(ctx context.Context, input port.IDPAuthInput) (*port.IDPAuthURL, error) {
	disc, err := p.getDiscovery(ctx)
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
	params.Set("response_type", "code")
	params.Set("client_id", p.cfg.ClientID)
	params.Set("redirect_uri", input.RedirectURI)
	params.Set("scope", strings.Join(scopes, " "))
	params.Set("state", input.State)
	params.Set("nonce", input.Nonce)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")

	authURL := disc.AuthorizationEndpoint + "?" + params.Encode()

	return &port.IDPAuthURL{
		URL:           authURL,
		State:         input.State,
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
	}, nil
}

// HandleCallback processes the OIDC callback: validates state, exchanges the code, and validates the id_token.
func (p *Provider) HandleCallback(ctx context.Context, input port.IDPCallbackInput) (*port.IDPCallbackResult, error) {
	if subtle.ConstantTimeCompare([]byte(input.State), []byte(input.ExpectedState)) == 0 {
		return nil, core.ErrInvalidIDPState
	}

	disc, err := p.getDiscovery(ctx)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: discovery failed: %w", err)
	}

	tokens, err := p.exchangeCode(ctx, disc.TokenEndpoint, input)
	if err != nil {
		return nil, fmt.Errorf("iam/oidc: token exchange failed: %w", err)
	}

	idpUser, err := p.validateIDToken(ctx, disc, tokens.IDToken)
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

func (p *Provider) getDiscovery(ctx context.Context) (*discoveryDoc, error) {
	var err error
	p.discOnce.Do(func() {
		discURL := strings.TrimRight(p.cfg.IssuerURL, "/") + "/.well-known/openid-configuration"
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
		if e != nil {
			err = e
			return
		}
		resp, e := p.client.Do(req)
		if e != nil {
			err = fmt.Errorf("iam/oidc: discovery request failed: %w", e)
			return
		}
		defer resp.Body.Close()

		var doc discoveryDoc
		if e := json.NewDecoder(resp.Body).Decode(&doc); e != nil {
			err = fmt.Errorf("iam/oidc: failed to decode discovery: %w", e)
			return
		}
		p.discovery = &doc
	})
	if err != nil {
		p.discOnce = sync.Once{} // allows retry
		return nil, err
	}
	return p.discovery, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (p *Provider) exchangeCode(ctx context.Context, tokenEndpoint string, input port.IDPCallbackInput) (*tokenResponse, error) {
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
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iam/oidc: token endpoint returned %d: %s", resp.StatusCode, string(b))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func (p *Provider) validateIDToken(ctx context.Context, disc *discoveryDoc, idToken string) (*types.IDPUser, error) {
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

	var signingMethod gojwt.SigningMethod
	switch alg {
	case "RS256":
		signingMethod = gojwt.SigningMethodRS256
	case "ES256":
		signingMethod = gojwt.SigningMethodES256
	default:
		signingMethod = gojwt.SigningMethodRS256
	}

	claims := gojwt.MapClaims{}
	parsed, err := gojwt.ParseWithClaims(idToken, claims, func(t *gojwt.Token) (any, error) {
		if t.Method.Alg() != signingMethod.Alg() {
			return nil, fmt.Errorf("unexpected alg: %s", t.Method.Alg())
		}
		return key, nil
	}, gojwt.WithIssuer(p.cfg.IssuerURL), gojwt.WithAudience(p.cfg.ClientID))
	if err != nil {
		return nil, fmt.Errorf("id_token validation failed: %w", err)
	}

	if !parsed.Valid {
		return nil, fmt.Errorf("id_token is invalid")
	}

	mc, _ := parsed.Claims.(gojwt.MapClaims)
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
	for k, v := range mc {
		raw[k] = v
	}

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

type jwksKey struct {
	Kid string
	Key any // *rsa.PublicKey or *ecdsa.PublicKey
}

type jwksCache struct {
	mu        sync.RWMutex
	keys      []jwksKey
	fetchedAt time.Time
	ttl       time.Duration
}

func (c *jwksCache) GetKey(ctx context.Context, client *http.Client, jwksURI, kid string) (any, error) {
	c.mu.RLock()
	if time.Since(c.fetchedAt) < c.ttl {
		key := c.findKey(kid)
		c.mu.RUnlock()
		if key != nil {
			return key, nil
		}
	} else {
		c.mu.RUnlock()
	}

	// Refetch keys from the JWKS endpoint.
	if err := c.fetch(ctx, client, jwksURI); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key := c.findKey(kid)
	if key == nil {
		return nil, fmt.Errorf("iam/oidc: key %q not found in JWKS", kid)
	}
	return key, nil
}

func (c *jwksCache) findKey(kid string) any {
	for _, k := range c.keys {
		if kid == "" || k.Kid == kid {
			return k.Key
		}
	}
	return nil
}

func (c *jwksCache) fetch(ctx context.Context, client *http.Client, jwksURI string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("iam/oidc: JWKS fetch failed: %w", err)
	}
	defer resp.Body.Close()

	var doc struct {
		Keys []jwkKey `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("iam/oidc: JWKS decode failed: %w", err)
	}

	var parsed []jwksKey
	for _, k := range doc.Keys {
		pub, err := k.PublicKey()
		if err != nil {
			continue // skip malformed keys
		}
		parsed = append(parsed, jwksKey{Kid: k.Kid, Key: pub})
	}

	c.mu.Lock()
	c.keys = parsed
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	return nil
}

// jwkKey represents a key in JWK (JSON Web Key) format.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (k *jwkKey) PublicKey() (any, error) {
	switch k.Kty {
	case "RSA":
		return k.rsaPublicKey()
	case "EC":
		return k.ecPublicKey()
	default:
		return nil, fmt.Errorf("unsupported key type: %s", k.Kty)
	}
}

func (k *jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	var eInt int
	for _, b := range eBytes {
		eInt = eInt*256 + int(b)
	}

	return &rsa.PublicKey{N: n, E: eInt}, nil
}

func (k *jwkKey) ecPublicKey() (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}

	pub := &ecdsa.PublicKey{
		X: new(big.Int).SetBytes(xBytes),
		Y: new(big.Int).SetBytes(yBytes),
	}

	switch k.Crv {
	case "P-256":
		pub.Curve = ellipticP256()
	case "P-384":
		pub.Curve = ellipticP384()
	case "P-521":
		pub.Curve = ellipticP521()
	default:
		return nil, fmt.Errorf("unsupported EC curve: %s", k.Crv)
	}

	return pub, nil
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

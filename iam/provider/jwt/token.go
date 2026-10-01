package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	gojwt "github.com/golang-jwt/jwt/v5"
)

// Algorithm defines the supported JWT signing algorithm.
type Algorithm string

const (
	HS256 Algorithm = "HS256"
	RS256 Algorithm = "RS256"
	ES256 Algorithm = "ES256"
)

// Config configures the JWT provider.
type Config struct {
	Algorithm Algorithm

	// HS256: symmetric key (minimum 32 bytes).
	Secret []byte

	// RS256/ES256: asymmetric key pair.
	PrivateKey any // *rsa.PrivateKey or *ecdsa.PrivateKey
	PublicKey  any // *rsa.PublicKey  or *ecdsa.PublicKey

	// KeyID is written as the kid header, identifying the signing key.
	KeyID string
	// VerificationKeys are previous keys, by kid, still accepted while tokens
	// they signed expire (key rotation). Same algorithm as the current key.
	VerificationKeys map[string]any

	AccessTokenTTL time.Duration // default: 15 min
	Issuer         string        // issued as iss
	VerifyIssuer   bool          // require iss == Issuer; opt-in for services sharing a secret
	Audience       string        // issued as aud and required on validation when set
	Leeway         time.Duration // clock skew tolerance for exp/iat/nbf
}

// iamClaims maps types.Claims to the JWT format with RegisteredClaims.
type iamClaims struct {
	gojwt.RegisteredClaims
	TenantID     string         `json:"tid"`
	Module       string         `json:"mod"`
	Roles        []string       `json:"roles"`
	SessionID    string         `json:"sid"`
	AuthProvider string         `json:"apv"`
	Extra        map[string]any `json:"ext,omitempty"`
}

// Provider implements port.TokenPort using JWT (HS256/RS256/ES256).
type Provider struct {
	cfg Config
}

// NewProvider builds a validated JWT Provider.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = HS256
	}

	if err := validateKeys(cfg); err != nil {
		return nil, err
	}
	if cfg.Leeway < 0 || cfg.Leeway > core.MaxClockSkew {
		return nil, core.ErrClockSkewExceeded
	}

	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = 15 * time.Minute
	}

	return &Provider{cfg: cfg}, nil
}

// IssueAccessToken issues a signed JWT access token with the provided claims.
func (p *Provider) IssueAccessToken(claims types.Claims) (string, error) {
	now := time.Now()
	exp := claims.ExpiresAt
	if exp.IsZero() {
		exp = now.Add(p.cfg.AccessTokenTTL)
	}

	jc := iamClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   claims.UserID,
			Issuer:    p.cfg.Issuer,
			Audience:  p.audience(),
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(exp),
		},
		TenantID:     claims.TenantID,
		Module:       claims.Module,
		Roles:        claims.Roles,
		SessionID:    claims.SessionID,
		AuthProvider: claims.AuthProvider,
		Extra:        claims.Extra,
	}

	token := gojwt.NewWithClaims(p.signingMethod(), jc)
	if p.cfg.KeyID != "" {
		token.Header["kid"] = p.cfg.KeyID
	}
	return token.SignedString(p.signingKey())
}

// IssueRefreshToken generates a high-entropy opaque token in the format {sessionID}.{random}.
// The core stores only the SHA-256 hash of the token.
func (p *Provider) IssueRefreshToken(claims types.Claims) (string, error) {
	_ = port.TokenPort(p) // compile-time interface check
	return buildRefreshToken(claims.SessionID)
}

// ParseToken validates signature and expiry. Does not validate the session — that is done by AuthPort.
func (p *Provider) ParseToken(token string) (*types.Claims, error) {
	parsed, err := gojwt.ParseWithClaims(token, &iamClaims{}, func(t *gojwt.Token) (any, error) {
		if t.Method.Alg() != string(p.cfg.Algorithm) {
			return nil, fmt.Errorf("iam/jwt: unexpected signing method: %s", t.Method.Alg())
		}
		return p.keyFor(t)
	}, p.parserOptions()...)
	if err != nil {
		if errors.Is(err, gojwt.ErrTokenExpired) {
			return nil, core.ErrTokenExpired
		}
		return nil, core.ErrTokenInvalid
	}

	jc, ok := parsed.Claims.(*iamClaims)
	if !ok || !parsed.Valid {
		return nil, core.ErrTokenInvalid
	}

	out := &types.Claims{
		UserID:       jc.Subject,
		TenantID:     jc.TenantID,
		Module:       jc.Module,
		Roles:        jc.Roles,
		SessionID:    jc.SessionID,
		AuthProvider: jc.AuthProvider,
		Issuer:       jc.Issuer,
		Extra:        jc.Extra,
	}
	if jc.IssuedAt != nil {
		out.IssuedAt = jc.IssuedAt.Time
	}
	if jc.ExpiresAt != nil {
		out.ExpiresAt = jc.ExpiresAt.Time
	}
	return out, nil
}

func (p *Provider) parserOptions() []gojwt.ParserOption {
	opts := []gojwt.ParserOption{
		gojwt.WithValidMethods([]string{string(p.cfg.Algorithm)}),
		gojwt.WithExpirationRequired(),
		gojwt.WithLeeway(p.cfg.Leeway),
	}
	if p.cfg.VerifyIssuer && p.cfg.Issuer != "" {
		opts = append(opts, gojwt.WithIssuer(p.cfg.Issuer))
	}
	if p.cfg.Audience != "" {
		opts = append(opts, gojwt.WithAudience(p.cfg.Audience))
	}
	return opts
}

func (p *Provider) audience() gojwt.ClaimStrings {
	if p.cfg.Audience == "" {
		return nil
	}
	return gojwt.ClaimStrings{p.cfg.Audience}
}

func (p *Provider) signingMethod() gojwt.SigningMethod {
	switch p.cfg.Algorithm {
	case RS256:
		return gojwt.SigningMethodRS256
	case ES256:
		return gojwt.SigningMethodES256
	default:
		return gojwt.SigningMethodHS256
	}
}

// signingKey returns the key NewProvider validated for the algorithm.
func (p *Provider) signingKey() any {
	switch p.cfg.Algorithm {
	case RS256, ES256:
		return p.cfg.PrivateKey
	default:
		return p.cfg.Secret
	}
}

// keyFor picks the key by kid; tokens without kid (issued before rotation
// was configured) are tried against every known key.
func (p *Provider) keyFor(t *gojwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	switch {
	case kid == "":
		if len(p.cfg.VerificationKeys) == 0 {
			return p.verificationKey(), nil
		}
		set := gojwt.VerificationKeySet{Keys: []gojwt.VerificationKey{p.verificationKey()}}
		for _, k := range p.cfg.VerificationKeys {
			set.Keys = append(set.Keys, k)
		}
		return set, nil
	case kid == p.cfg.KeyID:
		return p.verificationKey(), nil
	default:
		if k, ok := p.cfg.VerificationKeys[kid]; ok {
			return k, nil
		}
		return nil, fmt.Errorf("iam/jwt: unknown key id %q", kid)
	}
}

func (p *Provider) verificationKey() any {
	switch p.cfg.Algorithm {
	case RS256, ES256:
		return p.cfg.PublicKey
	default:
		return p.cfg.Secret
	}
}

// buildRefreshToken delegates to the internal implementation to keep logic centralized.
func buildRefreshToken(sessionID string) (string, error) {
	return buildRefreshTokenInternal(sessionID)
}

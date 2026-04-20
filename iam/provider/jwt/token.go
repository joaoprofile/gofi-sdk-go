package jwt

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/port"
	"github.com/joaoprofile/gofi/iam/types"
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

	AccessTokenTTL time.Duration // default: 15 min
	Issuer         string
}

// iamClaims maps types.Claims to the JWT format with RegisteredClaims.
type iamClaims struct {
	gojwt.RegisteredClaims
	TenantID     string   `json:"tid"`
	Module       string   `json:"mod"`
	Roles        []string `json:"roles"`
	SessionID    string   `json:"sid"`
	AuthProvider string   `json:"apv"`
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

	switch cfg.Algorithm {
	case HS256:
		if len(cfg.Secret) < 32 {
			return nil, core.ErrJWTSecretTooShort
		}
	case RS256, ES256:
		if cfg.PrivateKey == nil || cfg.PublicKey == nil {
			return nil, fmt.Errorf("iam/jwt: PrivateKey and PublicKey are required for %s", cfg.Algorithm)
		}
	default:
		return nil, fmt.Errorf("iam/jwt: unsupported algorithm %s", cfg.Algorithm)
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
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(exp),
		},
		TenantID:     claims.TenantID,
		Module:       claims.Module,
		Roles:        claims.Roles,
		SessionID:    claims.SessionID,
		AuthProvider: claims.AuthProvider,
	}

	token := gojwt.NewWithClaims(p.signingMethod(), jc)
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
		return p.verificationKey(), nil
	})
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

	return &types.Claims{
		UserID:       jc.Subject,
		TenantID:     jc.TenantID,
		Module:       jc.Module,
		Roles:        jc.Roles,
		SessionID:    jc.SessionID,
		AuthProvider: jc.AuthProvider,
		Issuer:       jc.Issuer,
		IssuedAt:     jc.IssuedAt.Time,
		ExpiresAt:    jc.ExpiresAt.Time,
	}, nil
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

func (p *Provider) signingKey() any {
	switch p.cfg.Algorithm {
	case RS256:
		return p.cfg.PrivateKey.(*rsa.PrivateKey)
	case ES256:
		return p.cfg.PrivateKey
	default:
		return p.cfg.Secret
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

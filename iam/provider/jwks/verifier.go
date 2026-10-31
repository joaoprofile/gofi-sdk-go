// Package jwks verifies JWTs signed by a third party that publishes its public
// keys as a JWKS: tokens a customer's system issues to its own users, for
// example. Only asymmetric algorithms are accepted; keys are cached per URL.
//
// The HTTP client fetches a URL configured by someone else: inject an
// outbound client that blocks private addresses (netx/httpx.NewSafeClient).
package jwks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
)

// DefaultAlgorithms are accepted when Config.Algorithms is empty.
var DefaultAlgorithms = []string{"RS256", "ES256"}

// asymmetric lists every algorithm a Verifier may accept.
var asymmetric = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512"}

// defaultClockSkew is the leeway when Config.ClockSkew is zero.
const defaultClockSkew = time.Minute

// ErrInvalidToken wraps every verification failure; the wrapped text names
// the reason (signature, issuer, audience, expiry, lifetime) and is safe to
// show to whoever configured the issuer, not to the token holder.
var ErrInvalidToken = errors.New("iam/jwks: invalid token")

// ErrInvalidConfig is returned by NewVerifier for an unusable Config.
var ErrInvalidConfig = errors.New("iam/jwks: invalid config")

// Config configures a Verifier.
type Config struct {
	// JWKSURL is the https URL of the issuer's key set.
	JWKSURL string

	// Issuer must equal the token's iss claim.
	Issuer string

	// Audience must be one of the token's aud values.
	Audience string

	// Algorithms accepted in the token header; asymmetric only.
	// Default: DefaultAlgorithms.
	Algorithms []string

	// ClockSkew is the leeway for exp, iat and nbf. Default: 1 minute;
	// capped at core.MaxClockSkew.
	ClockSkew time.Duration

	// MaxLifetime, when > 0, requires iat and rejects tokens whose exp - iat
	// exceeds it, so an issuer cannot mint long-lived tokens.
	MaxLifetime time.Duration

	// CacheTTL is how long the key set is served before a refetch.
	// Default: DefaultCacheTTL.
	CacheTTL time.Duration

	// HTTPClient fetches the key set. Default: a client with a 10s timeout.
	HTTPClient *http.Client
}

// Verifier checks tokens of one issuer against its JWKS.
type Verifier struct {
	cfg    Config
	client *http.Client
	keys   *Cache
}

// NewVerifier validates cfg and returns a Verifier. No request is made.
func NewVerifier(cfg Config) (*Verifier, error) {
	if err := RequireHTTPS(cfg.JWKSURL); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("%w: issuer and audience are required", ErrInvalidConfig)
	}
	if len(cfg.Algorithms) == 0 {
		cfg.Algorithms = DefaultAlgorithms
	}
	for _, alg := range cfg.Algorithms {
		if !slices.Contains(asymmetric, alg) {
			return nil, fmt.Errorf("%w: algorithm %q is not asymmetric", ErrInvalidConfig, alg)
		}
	}
	if cfg.ClockSkew <= 0 {
		cfg.ClockSkew = defaultClockSkew
	}
	cfg.ClockSkew = min(cfg.ClockSkew, core.MaxClockSkew)
	if cfg.MaxLifetime < 0 {
		return nil, fmt.Errorf("%w: negative MaxLifetime", ErrInvalidConfig)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Verifier{cfg: cfg, client: client, keys: NewCache(cfg.CacheTTL)}, nil
}

// Check fetches the key set now; use it to validate an issuer when it is
// configured. It returns the number of usable keys.
func (v *Verifier) Check(ctx context.Context) (int, error) {
	return v.keys.Refresh(ctx, v.client, v.cfg.JWKSURL)
}

// Verify checks signature, algorithm, issuer, audience, exp (required), iat,
// nbf and MaxLifetime, and returns the token's claims.
func (v *Verifier) Verify(ctx context.Context, raw string) (map[string]any, error) {
	unverified, _, err := gojwt.NewParser().ParseUnverified(raw, gojwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("%w: malformed token", ErrInvalidToken)
	}
	alg, _ := unverified.Header["alg"].(string)
	if !slices.Contains(v.cfg.Algorithms, alg) {
		return nil, fmt.Errorf("%w: algorithm %q not accepted", ErrInvalidToken, alg)
	}
	kid, _ := unverified.Header["kid"].(string)
	key, err := v.keys.GetKey(ctx, v.client, v.cfg.JWKSURL, kid)
	if err != nil {
		return nil, fmt.Errorf("%w: signing key unavailable: %w", ErrInvalidToken, err)
	}

	claims := gojwt.MapClaims{}
	opts := []gojwt.ParserOption{
		gojwt.WithValidMethods(v.cfg.Algorithms),
		gojwt.WithIssuer(v.cfg.Issuer),
		gojwt.WithAudience(v.cfg.Audience),
		gojwt.WithExpirationRequired(),
		gojwt.WithIssuedAt(),
		gojwt.WithLeeway(v.cfg.ClockSkew),
	}
	if _, err := gojwt.ParseWithClaims(raw, claims, func(*gojwt.Token) (any, error) { return key, nil }, opts...); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if err := v.checkLifetime(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func (v *Verifier) checkLifetime(claims gojwt.MapClaims) error {
	if v.cfg.MaxLifetime == 0 {
		return nil
	}
	iat, err := claims.GetIssuedAt()
	if err != nil || iat == nil {
		return fmt.Errorf("%w: iat is required", ErrInvalidToken)
	}
	exp, _ := claims.GetExpirationTime()
	if exp.Sub(iat.Time) > v.cfg.MaxLifetime {
		return fmt.Errorf("%w: lifetime exceeds %s", ErrInvalidToken, v.cfg.MaxLifetime)
	}
	return nil
}

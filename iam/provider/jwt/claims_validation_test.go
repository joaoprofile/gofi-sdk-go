package jwt

import (
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sharedSecret = []byte("a-32-byte-secret-key-for-testing!")

func newHS(t *testing.T, cfg Config) *Provider {
	t.Helper()
	cfg.Algorithm, cfg.Secret = HS256, sharedSecret
	p, err := NewProvider(cfg)
	require.NoError(t, err)
	return p
}

func issue(t *testing.T, p *Provider) string {
	t.Helper()
	tok, err := p.IssueAccessToken(types.Claims{UserID: "u1", SessionID: "s1"})
	require.NoError(t, err)
	return tok
}

func TestParseToken_RejectsOtherIssuer(t *testing.T) {
	other := newHS(t, Config{Issuer: "service-b"})
	p := newHS(t, Config{Issuer: "service-a", VerifyIssuer: true})

	_, err := p.ParseToken(issue(t, other))
	assert.ErrorIs(t, err, core.ErrTokenInvalid)
}

func TestParseToken_IssuerNotEnforcedByDefault(t *testing.T) {
	other := newHS(t, Config{Issuer: "service-b"})
	p := newHS(t, Config{Issuer: "service-a"})

	_, err := p.ParseToken(issue(t, other))
	assert.NoError(t, err, "existing deployments sharing a secret must keep working")
}

func TestParseToken_Audience(t *testing.T) {
	p := newHS(t, Config{Issuer: "gofi", Audience: "api-a"})

	_, err := p.ParseToken(issue(t, p))
	assert.NoError(t, err)

	_, err = p.ParseToken(issue(t, newHS(t, Config{Issuer: "gofi", Audience: "api-b"})))
	assert.ErrorIs(t, err, core.ErrTokenInvalid, "token for another audience")

	_, err = p.ParseToken(issue(t, newHS(t, Config{Issuer: "gofi"})))
	assert.ErrorIs(t, err, core.ErrTokenInvalid, "token without audience")
}

func TestParseToken_RejectsTokenWithoutExpiry(t *testing.T) {
	p := newHS(t, Config{Issuer: "gofi"})
	tok, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{"sub": "u1", "iss": "gofi"}).SignedString(sharedSecret)
	require.NoError(t, err)

	_, err = p.ParseToken(tok)
	assert.ErrorIs(t, err, core.ErrTokenInvalid)
}

func TestParseToken_Leeway(t *testing.T) {
	p := newHS(t, Config{Issuer: "gofi", Leeway: time.Minute})
	tok, err := p.IssueAccessToken(types.Claims{UserID: "u1", ExpiresAt: time.Now().Add(-10 * time.Second)})
	require.NoError(t, err)

	_, err = p.ParseToken(tok)
	assert.NoError(t, err)
}

package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A wrong key type used to panic at signing time; NewProvider must reject it.
func TestNewProvider_RejectsWrongKeyTypes(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rs, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	secret := []byte("a-32-byte-secret-key-for-testing!")

	cases := map[string]Config{
		"RS256 with ecdsa key":      {Algorithm: RS256, PrivateKey: ec, PublicKey: &ec.PublicKey},
		"ES256 with rsa key":        {Algorithm: ES256, PrivateKey: rs, PublicKey: &rs.PublicKey},
		"RS256 mismatched pair":     {Algorithm: RS256, PrivateKey: rs, PublicKey: &mustRSA(t, 2048).PublicKey},
		"ES256 mismatched pair":     {Algorithm: ES256, PrivateKey: ec, PublicKey: &mustEC(t, elliptic.P256()).PublicKey},
		"HS256 string verification": {Algorithm: HS256, Secret: secret, VerificationKeys: map[string]any{"old": string(secret)}},
		"RS256 hmac verification":   {Algorithm: RS256, PrivateKey: rs, PublicKey: &rs.PublicKey, VerificationKeys: map[string]any{"old": secret}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewProvider(cfg)
			assert.ErrorIs(t, err, ErrInvalidKey)
		})
	}
}

func TestNewProvider_RejectsWeakKeys(t *testing.T) {
	small := mustRSA(t, 1024)
	_, err := NewProvider(Config{Algorithm: RS256, PrivateKey: small, PublicKey: &small.PublicKey})
	assert.ErrorIs(t, err, ErrInvalidKey, "RSA below 2048 bits")

	p384 := mustEC(t, elliptic.P384())
	_, err = NewProvider(Config{Algorithm: ES256, PrivateKey: p384, PublicKey: &p384.PublicKey})
	assert.ErrorIs(t, err, ErrInvalidKey, "ES256 needs P-256")

	_, err = NewProvider(Config{Secret: []byte("a-32-byte-secret-key-for-testing!"), VerificationKeys: map[string]any{"old": []byte("short")}})
	assert.ErrorIs(t, err, core.ErrJWTSecretTooShort)
}

func TestNewProvider_CapsLeeway(t *testing.T) {
	_, err := NewProvider(Config{Secret: []byte("a-32-byte-secret-key-for-testing!"), Leeway: 6 * time.Minute})
	assert.ErrorIs(t, err, core.ErrClockSkewExceeded)
	_, err = NewProvider(Config{Secret: []byte("a-32-byte-secret-key-for-testing!"), Leeway: 5 * time.Minute})
	assert.NoError(t, err)
}

func mustRSA(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits) // #nosec G403 -- small keys only to test rejection
	require.NoError(t, err)
	return k
}

func mustEC(t *testing.T, c elliptic.Curve) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(c, rand.Reader)
	require.NoError(t, err)
	return k
}

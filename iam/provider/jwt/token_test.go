package jwt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider_HS256_Success(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProvider_HS256_SecretTooShort(t *testing.T) {
	_, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("short"),
	})
	assert.ErrorIs(t, err, core.ErrJWTSecretTooShort)
}

func TestNewProvider_DefaultAlgorithmIsHS256(t *testing.T) {
	p, err := NewProvider(Config{
		Secret: []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)
	assert.Equal(t, HS256, p.cfg.Algorithm)
}

func TestNewProvider_RS256_Success(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	p, err := NewProvider(Config{
		Algorithm:  RS256,
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProvider_RS256_MissingKeys(t *testing.T) {
	_, err := NewProvider(Config{
		Algorithm: RS256,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PrivateKey and PublicKey are required")
}

func TestNewProvider_ES256_Success(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	p, err := NewProvider(Config{
		Algorithm:  ES256,
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewProvider_ES256_MissingKeys(t *testing.T) {
	_, err := NewProvider(Config{
		Algorithm: ES256,
	})
	assert.Error(t, err)
}

func TestNewProvider_UnsupportedAlgorithm(t *testing.T) {
	_, err := NewProvider(Config{
		Algorithm: "PS256",
		Secret:    []byte("secret"),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestNewProvider_DefaultAccessTokenTTL(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, p.cfg.AccessTokenTTL)
}

func TestProvider_IssueAndParseAccessToken_HS256(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm:      HS256,
		Secret:         []byte("a-32-byte-secret-key-for-testing!"),
		AccessTokenTTL: time.Minute,
		Issuer:         "test-issuer",
	})
	require.NoError(t, err)

	claims := types.Claims{
		UserID:       "user-1",
		TenantID:     "tenant-1",
		Module:       "module-a",
		Roles:        []string{"admin", "viewer"},
		SessionID:    "session-abc",
		AuthProvider: "local",
		ExpiresAt:    time.Now().Add(time.Minute),
	}

	token, err := p.IssueAccessToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.True(t, strings.Count(token, ".") == 2) // JWT has 3 parts

	parsed, err := p.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", parsed.UserID)
	assert.Equal(t, "tenant-1", parsed.TenantID)
	assert.Equal(t, "module-a", parsed.Module)
	assert.Equal(t, []string{"admin", "viewer"}, parsed.Roles)
	assert.Equal(t, "session-abc", parsed.SessionID)
	assert.Equal(t, "local", parsed.AuthProvider)
}

func TestProvider_ParseToken_ExpiredToken(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm:      HS256,
		Secret:         []byte("a-32-byte-secret-key-for-testing!"),
		AccessTokenTTL: time.Minute,
	})
	require.NoError(t, err)

	claims := types.Claims{
		UserID:    "u1",
		SessionID: "s1",
		ExpiresAt: time.Now().Add(-time.Hour), // already expired
	}

	token, err := p.IssueAccessToken(claims)
	require.NoError(t, err)

	_, err = p.ParseToken(token)
	assert.ErrorIs(t, err, core.ErrTokenExpired)
}

func TestProvider_ParseToken_TamperedToken(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)

	_, err = p.ParseToken("not.a.valid.jwt")
	assert.ErrorIs(t, err, core.ErrTokenInvalid)
}

func TestProvider_ParseToken_WrongSecret(t *testing.T) {
	p1, _ := NewProvider(Config{
		Algorithm:      HS256,
		Secret:         []byte("a-32-byte-secret-key-for-testing!"),
		AccessTokenTTL: time.Minute,
	})
	p2, _ := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("different-32-byte-secret-for-test"),
	})

	claims := types.Claims{
		UserID:    "u1",
		SessionID: "s1",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	token, err := p1.IssueAccessToken(claims)
	require.NoError(t, err)

	_, err = p2.ParseToken(token)
	assert.ErrorIs(t, err, core.ErrTokenInvalid)
}

func TestProvider_IssueRefreshToken_Format(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)

	claims := types.Claims{SessionID: "my-session-id"}
	token, err := p.IssueRefreshToken(claims)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, "my-session-id."))
}

func TestProvider_IssueRefreshToken_Uniqueness(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)

	claims := types.Claims{SessionID: "sess"}
	t1, _ := p.IssueRefreshToken(claims)
	t2, _ := p.IssueRefreshToken(claims)
	assert.NotEqual(t, t1, t2)
}

func TestProvider_RS256_IssueAndParse(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	p, err := NewProvider(Config{
		Algorithm:      RS256,
		PrivateKey:     priv,
		PublicKey:      &priv.PublicKey,
		AccessTokenTTL: time.Minute,
	})
	require.NoError(t, err)

	claims := types.Claims{
		UserID:    "u1",
		SessionID: "s1",
		ExpiresAt: time.Now().Add(time.Minute),
	}

	token, err := p.IssueAccessToken(claims)
	require.NoError(t, err)

	parsed, err := p.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "u1", parsed.UserID)
}

func TestProvider_ES256_IssueAndParse(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	p, err := NewProvider(Config{
		Algorithm:      ES256,
		PrivateKey:     priv,
		PublicKey:      &priv.PublicKey,
		AccessTokenTTL: time.Minute,
	})
	require.NoError(t, err)

	claims := types.Claims{
		UserID:    "u1",
		SessionID: "s1",
		ExpiresAt: time.Now().Add(time.Minute),
	}

	token, err := p.IssueAccessToken(claims)
	require.NoError(t, err)

	parsed, err := p.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, "u1", parsed.UserID)
}

func TestProvider_ParseToken_CrossAlgorithmAttack(t *testing.T) {
	// Token signed with ES256 must not be parseable by HS256 provider.
	privEC, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	pEC, err := NewProvider(Config{
		Algorithm:      ES256,
		PrivateKey:     privEC,
		PublicKey:      &privEC.PublicKey,
		AccessTokenTTL: time.Minute,
	})
	require.NoError(t, err)

	pHS, err := NewProvider(Config{
		Algorithm: HS256,
		Secret:    []byte("a-32-byte-secret-key-for-testing!"),
	})
	require.NoError(t, err)

	claims := types.Claims{UserID: "u1", SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}
	token, err := pEC.IssueAccessToken(claims)
	require.NoError(t, err)

	_, err = pHS.ParseToken(token)
	assert.ErrorIs(t, err, core.ErrTokenInvalid)
}

func TestProvider_IssueAccessToken_UsesClaimsExpiresAt(t *testing.T) {
	p, err := NewProvider(Config{
		Algorithm:      HS256,
		Secret:         []byte("a-32-byte-secret-key-for-testing!"),
		AccessTokenTTL: time.Minute,
	})
	require.NoError(t, err)

	customExp := time.Now().Add(30 * time.Minute)
	claims := types.Claims{
		UserID:    "u1",
		SessionID: "s1",
		ExpiresAt: customExp,
	}

	token, err := p.IssueAccessToken(claims)
	require.NoError(t, err)

	parsed, err := p.ParseToken(token)
	require.NoError(t, err)
	// Should be approximately 30 minutes from now, not 1 minute.
	assert.True(t, parsed.ExpiresAt.After(time.Now().Add(25*time.Minute)))
}

package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecureToken_ReturnsNonEmptyString(t *testing.T) {
	token, err := generateSecureToken(32)
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestGenerateSecureToken_DifferentCallsReturnDifferentTokens(t *testing.T) {
	a, err := generateSecureToken(32)
	require.NoError(t, err)
	b, err := generateSecureToken(32)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestGenerateSecureToken_LengthProducesExpectedOutput(t *testing.T) {
	token, err := generateSecureToken(16)
	require.NoError(t, err)
	// 16 bytes in base64url without padding is ceiling(16*8/6) = 22 chars
	assert.GreaterOrEqual(t, len(token), 20)
}

func TestHashToken_IsDeterministic(t *testing.T) {
	h1 := hashToken("my-token")
	h2 := hashToken("my-token")
	assert.Equal(t, h1, h2)
}

func TestHashToken_DifferentInputsProduceDifferentHashes(t *testing.T) {
	h1 := hashToken("token-a")
	h2 := hashToken("token-b")
	assert.NotEqual(t, h1, h2)
}

func TestHashToken_IsHexString(t *testing.T) {
	h := hashToken("anything")
	assert.Len(t, h, 64) // SHA-256 produces 32 bytes = 64 hex chars
	for _, c := range h {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'), "unexpected char: %c", c)
	}
}

func TestLastFour_LongString(t *testing.T) {
	assert.Equal(t, "abcd", lastFour("xyzabcd"))
}

func TestLastFour_ExactlyFourChars(t *testing.T) {
	assert.Equal(t, "abcd", lastFour("abcd"))
}

func TestLastFour_ShorterThanFour(t *testing.T) {
	assert.Equal(t, "ab", lastFour("ab"))
}

func TestLastFour_EmptyString(t *testing.T) {
	assert.Equal(t, "", lastFour(""))
}

func TestGeneratePKCE_ReturnsNonEmptyPair(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	require.NoError(t, err)
	assert.NotEmpty(t, verifier)
	assert.NotEmpty(t, challenge)
}

func TestGeneratePKCE_VerifierAndChallengeDiffer(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	require.NoError(t, err)
	assert.NotEqual(t, verifier, challenge)
}

func TestGeneratePKCE_DifferentCallsReturnDifferentPairs(t *testing.T) {
	v1, c1, err := generatePKCE()
	require.NoError(t, err)
	v2, c2, err := generatePKCE()
	require.NoError(t, err)
	assert.NotEqual(t, v1, v2)
	assert.NotEqual(t, c1, c2)
}

func TestGenerateState_ReturnsNonEmptyString(t *testing.T) {
	state, err := generateState()
	require.NoError(t, err)
	assert.NotEmpty(t, state)
}

func TestGenerateState_DifferentCallsReturnDifferentValues(t *testing.T) {
	a, _ := generateState()
	b, _ := generateState()
	assert.NotEqual(t, a, b)
}

func TestGenerateNonce_ReturnsNonEmptyString(t *testing.T) {
	nonce, err := generateNonce()
	require.NoError(t, err)
	assert.NotEmpty(t, nonce)
}

func TestBuildRefreshToken_ContainsSessionID(t *testing.T) {
	sessionID := "my-session-id"
	token, err := buildRefreshToken(sessionID)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, sessionID+"."))
}

func TestBuildRefreshToken_DifferentCallsReturnDifferentTokens(t *testing.T) {
	a, err := buildRefreshToken("session-1")
	require.NoError(t, err)
	b, err := buildRefreshToken("session-1")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestParseRefreshToken_ValidToken(t *testing.T) {
	sessionID := "my-session-id"
	token, err := buildRefreshToken(sessionID)
	require.NoError(t, err)

	parsed, err := parseRefreshToken(token)
	require.NoError(t, err)
	assert.Equal(t, sessionID, parsed)
}

func TestParseRefreshToken_MissingDotReturnsError(t *testing.T) {
	_, err := parseRefreshToken("invalidsession")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestParseRefreshToken_EmptyStringReturnsError(t *testing.T) {
	_, err := parseRefreshToken("")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestParseRefreshToken_LeadingDotReturnsError(t *testing.T) {
	_, err := parseRefreshToken(".randompart")
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestParseRefreshToken_ExtractsCorrectSessionID(t *testing.T) {
	sessionID := "uuid-1234-abcd"
	token := sessionID + ".randomsuffix"
	parsed, err := parseRefreshToken(token)
	require.NoError(t, err)
	assert.Equal(t, sessionID, parsed)
}

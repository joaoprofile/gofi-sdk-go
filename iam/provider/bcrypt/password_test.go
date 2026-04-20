package bcrypt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHash_ValidPassword(t *testing.T) {
	hash, err := Hash("my-password", MinCost)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$"))
}

func TestHash_CostBelowMinimumReturnsError(t *testing.T) {
	_, err := Hash("password", MinCost-1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "below minimum")
}

func TestHash_DifferentPasswordsProduceDifferentHashes(t *testing.T) {
	h1, _ := Hash("password-a", MinCost)
	h2, _ := Hash("password-b", MinCost)
	assert.NotEqual(t, h1, h2)
}

func TestHash_SamePasswordProducesDifferentHashesDueToSalt(t *testing.T) {
	h1, _ := Hash("same-password", MinCost)
	h2, _ := Hash("same-password", MinCost)
	assert.NotEqual(t, h1, h2)
}

func TestHashDefault_UsesDefaultCost(t *testing.T) {
	hash, err := HashDefault("my-password")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestHashDefault_CanBeVerifiedByCompare(t *testing.T) {
	hash, err := HashDefault("my-password")
	require.NoError(t, err)
	err = Compare(hash, "my-password")
	assert.NoError(t, err)
}

func TestCompare_CorrectPassword(t *testing.T) {
	hash, err := Hash("correct-password", MinCost)
	require.NoError(t, err)
	err = Compare(hash, "correct-password")
	assert.NoError(t, err)
}

func TestCompare_WrongPassword(t *testing.T) {
	hash, err := Hash("correct-password", MinCost)
	require.NoError(t, err)
	err = Compare(hash, "wrong-password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestCompare_InvalidHash(t *testing.T) {
	err := Compare("not-a-hash", "password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

func TestCompare_EmptyPasswordFails(t *testing.T) {
	hash, err := Hash("a-password", MinCost)
	require.NoError(t, err)
	err = Compare(hash, "")
	assert.Error(t, err)
}

func TestCompare_DoesNotLeakDetails(t *testing.T) {
	err := Compare("not-a-hash", "password")
	assert.Equal(t, "iam/bcrypt: invalid credentials", err.Error())
}

func TestConstants_DefaultCostIsAtLeastMinCost(t *testing.T) {
	assert.GreaterOrEqual(t, DefaultCost, MinCost)
	assert.Equal(t, 12, DefaultCost)
}

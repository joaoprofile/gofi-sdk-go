package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// generateSecureToken generates a high-entropy random token with n bytes.
// Returns a base64url-safe string without padding.
func generateSecureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("iam: failed to generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken computes the SHA-256 of the token and returns it as a hex string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// lastFour returns the last 4 characters of a string, or fewer if shorter.
func lastFour(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

// generatePKCE generates a code_verifier and code_challenge pair for PKCE (RFC 7636).
// The verifier is 64 random bytes encoded as base64url (approximately 86 chars).
// The challenge is SHA-256(verifier) encoded as base64url without padding.
func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 64)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("iam: failed to generate PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// generateState generates a 32-byte random state value for OAuth2 CSRF protection.
func generateState() (string, error) {
	return generateSecureToken(32)
}

// generateNonce generates a 16-byte random nonce for OIDC.
func generateNonce() (string, error) {
	return generateSecureToken(16)
}

// buildRefreshToken builds a refresh token in the format {sessionID}.{randomBase64url}.
// This format allows extracting the sessionID without a secondary index.
func buildRefreshToken(sessionID string) (string, error) {
	randomPart, err := generateSecureToken(32)
	if err != nil {
		return "", err
	}
	return sessionID + "." + randomPart, nil
}

// parseRefreshToken extracts the sessionID from a refresh token in the format {sessionID}.{random}.
func parseRefreshToken(token string) (sessionID string, err error) {
	idx := strings.Index(token, ".")
	if idx <= 0 {
		return "", ErrTokenInvalid
	}
	return token[:idx], nil
}

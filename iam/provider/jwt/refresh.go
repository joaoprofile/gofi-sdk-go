package jwt

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// buildRefreshTokenInternal generates an opaque refresh token in the format {sessionID}.{random}.
// Separated from core to avoid a circular dependency between provider/jwt and core.
func buildRefreshTokenInternal(sessionID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("iam/jwt: failed to generate refresh token: %w", err)
	}
	return sessionID + "." + base64.RawURLEncoding.EncodeToString(b), nil
}

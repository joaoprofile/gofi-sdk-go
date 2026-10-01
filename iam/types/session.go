package types

import "time"

// Session represents an authenticated user session.
// The raw RefreshToken is only populated at issuance time and must never be persisted.
// What is persisted is the RefreshTokenHash, which is the SHA-256 of the raw token.
type Session struct {
	ID     string
	UserID string

	// Context selected after authentication.
	TenantID string
	Module   string

	// Access token issued for this session.
	AccessToken string

	// RefreshToken is the raw high-entropy token.
	// Populated only at issuance (SelectTenant and RefreshToken).
	// Never persisted — the SessionPort stores only RefreshTokenHash.
	RefreshToken string `json:"-"`

	// RefreshTokenHash is the SHA-256 of the raw token and is the value that gets persisted.
	RefreshTokenHash string

	// RefreshTokenLastFour is the suffix used for debugging and auditing without exposing the token.
	RefreshTokenLastFour string

	AuthProvider string // "local", "google", "github", etc.

	// AuthTime is the original login; kept across refresh rotations to
	// enforce SecurityConfig.SessionMaxLifetime.
	AuthTime time.Time

	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastUsedAt time.Time

	Revoked   bool
	RevokedAt *time.Time
	RevokedBy string // "user", "admin", "system", "token_rotation"

	// Audit context metadata.
	IPAddress string
	UserAgent string
	DeviceID  string

	// ClaimsExtra is re-issued as the "ext" claim of every access token of the
	// session, readable by any bearer: never secrets. Stored under the legacy
	// "extra" key so sessions saved before the split keep their claims.
	ClaimsExtra map[string]string `json:"extra,omitempty"`

	// SessionExtra carries server-side attributes (e.g. external provider
	// tokens). Persisted with the session, never put in a token.
	SessionExtra map[string]string `json:"session_extra,omitempty"`
}

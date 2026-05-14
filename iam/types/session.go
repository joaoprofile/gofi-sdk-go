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

	// Extra carries project-specific session attributes that the SDK schema does not
	// model (e.g. external provider tokens, domain role labels). Persisted by the
	// SessionPort as part of the session blob — round-tripped on Save/Get.
	Extra map[string]string `json:"extra,omitempty"`
}

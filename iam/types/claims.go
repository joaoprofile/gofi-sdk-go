package types

import "time"

// Claims represents the payload of a validated access token.
// It is the central object carried via context between middleware and handlers.
type Claims struct {
	UserID    string   `json:"sub"`
	TenantID  string   `json:"tid"`
	Module    string   `json:"mod"`
	Roles     []string `json:"roles"`
	SessionID string   `json:"sid"` // links the token to the session for revocation

	// Provider that originated the authentication such as "local", "google", or "github".
	AuthProvider string `json:"apv"`

	Issuer    string    `json:"iss"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`

	// Extra carries project-specific custom claims that the SDK schema does not model.
	// Round-tripped through the JWT payload under the "ext" key — preserved across
	// Issue → Parse so the application can recover its domain claims after the SDK
	// validates the token. Values must be JSON-serializable.
	Extra map[string]any `json:"ext,omitempty"`
}

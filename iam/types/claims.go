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
}

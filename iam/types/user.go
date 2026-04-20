package types

import "time"

// User represents a system user.
// PasswordHash must never be exposed outside the infrastructure layer.
type User struct {
	ID            string
	Email         string
	PasswordHash  string // bcrypt or argon2 hash — never plaintext
	Active        bool
	EmailVerified bool

	// External identities linked for social login via IDP.
	ExternalIdentities []ExternalIdentity
}

// ExternalIdentity represents an identity link with an external IDP.
type ExternalIdentity struct {
	Provider   string // "google", "github", "microsoft"
	ExternalID string // the user's ID at the external provider
	Email      string // email returned by the provider
	LinkedAt   time.Time
}

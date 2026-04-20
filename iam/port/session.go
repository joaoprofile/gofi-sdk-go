package port

import (
	"context"

	"github.com/joaoprofile/gofi/iam/types"
)

// SessionPort is the only port without an optional implementation.
// A session store is mandatory for real logout, refresh token rotation, and revocation.
// Built-in implementations: provider/redis for production, provider/memory for development and tests.
type SessionPort interface {
	// Save persists the session. The raw Session.RefreshToken field must never be persisted.
	Save(ctx context.Context, session *types.Session) error

	// Get retrieves a session by ID. Returns ErrSessionNotFound if it does not exist.
	Get(ctx context.Context, sessionID string) (*types.Session, error)

	// Revoke invalidates a specific session for single-device logout.
	Revoke(ctx context.Context, sessionID string) error

	// RevokeAllForUser invalidates all sessions for the user across all devices.
	RevokeAllForUser(ctx context.Context, userID string) error

	// ListByUser returns the active sessions for a user for device management.
	ListByUser(ctx context.Context, userID string) ([]*types.Session, error)
}

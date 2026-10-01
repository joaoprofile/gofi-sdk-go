package port

import (
	"context"
	"time"
)

// LoginAttempt identifies a login attempt for throttling. Email is already
// normalized (trimmed, lower-case); IPAddress may be empty.
type LoginAttempt struct {
	Email     string
	IPAddress string
}

// LoginThrottler limits password guessing. The built-in login calls Allow
// before touching the user store or hashing, then Failure or Success.
// Built-in implementations: provider/redis and provider/memory.
type LoginThrottler interface {
	// Allow returns core.ErrTooManyAttempts while the email or the IP is locked out.
	Allow(ctx context.Context, attempt LoginAttempt) error
	// Failure records a failed attempt and starts the lockout when a limit is reached.
	Failure(ctx context.Context, attempt LoginAttempt) error
	// Success clears the failures counted for the email.
	Success(ctx context.Context, attempt LoginAttempt) error
}

// TicketStore makes tenant tickets single-use. Consume records jti for ttl
// and reports false when it was already consumed (SETNX semantics).
// Built-in implementations: provider/redis and provider/memory.
type TicketStore interface {
	Consume(ctx context.Context, jti string, ttl time.Duration) (bool, error)
}

// UserRevocationStore is optionally implemented by a SessionPort.
// RevokeAllForUser records the cut-off first; RevokedBefore returns it (zero
// when never set), and sessions whose login (AuthTime) is not after it are
// rejected on validation and refresh, closing the race with a concurrent refresh.
type UserRevocationStore interface {
	RevokedBefore(ctx context.Context, userID string) (time.Time, error)
}

// DummyPasswordVerifier is optionally implemented by a UserPort. When the
// email is unknown, the built-in login calls it instead of the default dummy
// check, so both paths cost the same as ValidatePassword.
type DummyPasswordVerifier interface {
	DummyValidatePassword(ctx context.Context, password string)
}

package session

import (
	"context"
	"time"
)

// Driver is the pluggable storage backend contract.
// Implement this interface to add new session backends (Redis, OCI, DynamoDB, in-memory, etc.).
type Driver interface {
	Save(ctx context.Context, key string, entry *Entry) error
	Get(ctx context.Context, key string) (*Entry, error)
	Delete(ctx context.Context, key string) error
	ScanAll(ctx context.Context, prefix string) ([]string, error)
	CleanExpired(ctx context.Context, prefix string) error
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string) error
	IsLocked(ctx context.Context, key string) (bool, error)
}

// DistributedLocker is the contract for distributed locking.
// *Session implements this interface, so it can be injected wherever a DistributedLocker is expected.
type DistributedLocker interface {
	// TryLock returns the owner token of the acquired lock; pass it to Unlock.
	TryLock(ctx context.Context, key string) (token string, ok bool, err error)
	// Unlock releases the lock only while token still owns it.
	Unlock(ctx context.Context, key, token string) error
	IsLocked(ctx context.Context, key string) (bool, error)
	WithLock(ctx context.Context, key string, fn func() error) (bool, error)
}

// TokenLocker is optionally implemented by a Driver so that a lock can only be
// released by the holder that acquired it, even after its TTL expired.
type TokenLocker interface {
	AcquireLockToken(ctx context.Context, key string, ttl time.Duration) (token string, ok bool, err error)
	ReleaseLockToken(ctx context.Context, key, token string) error
}

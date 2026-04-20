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
	TryLock(ctx context.Context, key string) (bool, error)
	Unlock(ctx context.Context, key string) error
	IsLocked(ctx context.Context, key string) (bool, error)
	WithLock(ctx context.Context, key string, fn func() error) (bool, error)
}

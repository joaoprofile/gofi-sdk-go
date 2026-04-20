package session

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	defaultRetryDelay = 30 * time.Millisecond
	defaultAcquireTTL = 10 * time.Second
)

var (
	once     sync.Once
	instance *Session
)

// Session is the unified session manager and distributed locker.
// It delegates all storage operations to a pluggable Driver, making
// it easy to swap backends (Redis, OCI, in-memory, etc.) without
// changing any consumer code.
//
// *Session implements DistributedLocker, so it can be injected as a
// lock provider wherever that interface is expected.
type Session struct {
	driver Driver
	Prefix string
	TTL    time.Duration
}

// New initialises the Session singleton. Subsequent calls are no-ops;
// the first call wins. Pass a different Driver to switch backends.
func New(driver Driver, cfg *Config) *Session {
	once.Do(func() {
		instance = &Session{
			driver: driver,
			Prefix: cfg.Prefix,
			TTL:    cfg.TTL,
		}
	})
	return instance
}

// Instance returns the singleton Session, or nil if New has not been called.
func Instance() *Session {
	return instance
}

// --- internal locking helpers ---

// withLock acquires a namespaced internal lock (prefix:lock:key) so that
// session operations are race-free. Falls back to retry loop on contention.
func (s *Session) withLock(ctx context.Context, key string, fn func() error) error {
	lockKey := fmt.Sprintf("%s:lock:%s", s.Prefix, key)

	for {
		locked, err := s.driver.AcquireLock(ctx, lockKey, defaultAcquireTTL)
		if err != nil {
			return fmt.Errorf("failed to acquire lock: %w", err)
		}

		if locked {
			defer s.driver.ReleaseLock(ctx, lockKey)
			return fn()
		}

		select {
		case <-time.After(defaultRetryDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Session) newEntry(data SessionData) *Entry {
	now := time.Now()
	return &Entry{
		ID:        GenerateSessionID(),
		Data:      data,
		CreatedAt: now,
		ExpiresAt: now.Add(s.TTL),
	}
}

// --- Session management ---

// CreateOrGet returns an existing, non-expired entry for key, or creates and
// stores a new one from data — all under a distributed lock.
func (s *Session) CreateOrGet(ctx context.Context, key string, data SessionData) (*Entry, error) {
	var result *Entry

	err := s.withLock(ctx, key, func() error {
		existing, err := s.driver.Get(ctx, key)
		if err != nil {
			return err
		}

		if existing != nil && !existing.IsExpired() {
			result = existing
			return nil
		}

		entry := s.newEntry(data)
		if err := s.driver.Save(ctx, key, entry); err != nil {
			return err
		}

		result = entry
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// Force overwrites any existing entry under key with fresh data.
func (s *Session) Force(ctx context.Context, key string, data SessionData) (*Entry, error) {
	var result *Entry

	err := s.withLock(ctx, key, func() error {
		entry := s.newEntry(data)
		if err := s.driver.Save(ctx, key, entry); err != nil {
			return fmt.Errorf("failed to force-save entry: %w", err)
		}
		result = entry
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// Get retrieves an entry by key without creating one (no lock needed).
func (s *Session) Get(ctx context.Context, key string) (*Entry, error) {
	return s.driver.Get(ctx, key)
}

// Delete removes an entry by key.
func (s *Session) Delete(ctx context.Context, key string) error {
	return s.driver.Delete(ctx, key)
}

// CleanExpired removes all expired entries whose keys match prefix.
func (s *Session) CleanExpired(ctx context.Context, prefix string) error {
	return s.driver.CleanExpired(ctx, prefix)
}

// --- DistributedLocker implementation ---

// TryLock attempts to acquire a distributed lock for key.
// The key is used as-is; callers should use NewKey to build
// collision-resistant identifiers.
func (s *Session) TryLock(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, ErrInvalidKey
	}
	return s.driver.AcquireLock(ctx, key, defaultLockTTL)
}

// Unlock releases the distributed lock for key.
func (s *Session) Unlock(ctx context.Context, key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return s.driver.ReleaseLock(ctx, key)
}

// IsLocked reports whether key is currently locked.
func (s *Session) IsLocked(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, ErrInvalidKey
	}
	return s.driver.IsLocked(ctx, key)
}

// WithLock acquires the lock, runs fn, then releases it.
// Returns (false, nil) if the key is already locked.
// Returns (true, err) if fn returned an error (lock is still released).
func (s *Session) WithLock(ctx context.Context, key string, fn func() error) (bool, error) {
	ok, err := s.TryLock(ctx, key)
	if err != nil {
		return false, fmt.Errorf("error when trying to lock key %s: %w", key, err)
	}

	if !ok {
		return false, nil
	}

	defer s.Unlock(ctx, key)

	if err := fn(); err != nil {
		return true, err
	}

	return true, nil
}

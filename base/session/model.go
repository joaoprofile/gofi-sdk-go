package session

import (
	"errors"
	"time"
)

const (
	defaultLockTTL    = 10 * time.Second
	defaultSessionTTL = 10 * time.Minute
	defaultPrefix     = "session"

	StateProcessing = "processing"
	StateDone       = "done"
	StateFailed     = "failed"
)

var (
	ErrAlreadyLocked  = errors.New("resource is already locked")
	ErrNotLocked      = errors.New("resource is not locked")
	ErrInvalidKey     = errors.New("invalid key for locking")
	ErrEncodingFailed = errors.New("failed to encode lock object")
)

// LockEntry is the value stored in the backend when a lock is held.
type LockEntry struct {
	Key   string `json:"key"`
	State string `json:"state"`
}

// SessionData holds the arbitrary key-value payload of an entry.
type SessionData map[string]any

// Entry is the session record stored by a Driver.
type Entry struct {
	ID        string      `json:"id"`
	Data      SessionData `json:"data"`
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
}

func (e *Entry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

func (e *Entry) TTL() time.Duration {
	return time.Until(e.ExpiresAt)
}

func (e *Entry) Extend(d time.Duration) {
	e.ExpiresAt = time.Now().Add(d)
}

func (e *Entry) Get(key string) any {
	if e.Data == nil {
		return nil
	}
	return e.Data[key]
}

func (e *Entry) Set(key string, value any) *Entry {
	if e.Data == nil {
		e.Data = make(SessionData)
	}
	e.Data[key] = value
	return e
}

// Config holds the session manager configuration.
type Config struct {
	TTL    time.Duration
	Prefix string
}

// DefaultSessionConfig returns a Config with sensible defaults
// (10-minute TTL, "session" prefix).
func DefaultSessionConfig() *Config {
	return &Config{
		TTL:    defaultSessionTTL,
		Prefix: defaultPrefix,
	}
}

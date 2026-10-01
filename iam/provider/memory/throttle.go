package memory

import (
	"context"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
)

// ThrottleConfig configures a LoginThrottler. Zero values use the defaults.
type ThrottleConfig struct {
	MaxAttempts   int           // failures per email within Window; default 5
	IPMaxAttempts int           // failures per IP within Window; default 4x MaxAttempts
	Window        time.Duration // sliding window; default 15 min
	Lockout       time.Duration // lockout once a limit is reached; default 15 min
}

func (c ThrottleConfig) withDefaults() ThrottleConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.IPMaxAttempts <= 0 {
		c.IPMaxAttempts = 4 * c.MaxAttempts
	}
	if c.Window <= 0 {
		c.Window = 15 * time.Minute
	}
	if c.Lockout <= 0 {
		c.Lockout = 15 * time.Minute
	}
	return c
}

// sweepAt bounds the maps: expired entries are dropped once they grow past it.
const sweepAt = 10_000

// LoginThrottler implements port.LoginThrottler in memory with a sliding
// window per email and per IP. Single instance only; use provider/redis with
// several instances.
type LoginThrottler struct {
	cfg      ThrottleConfig
	mu       sync.Mutex
	failures map[string][]time.Time
	locked   map[string]time.Time // key -> lockout end
	now      func() time.Time
}

// NewLoginThrottler builds an in-memory LoginThrottler.
func NewLoginThrottler(cfg ThrottleConfig) *LoginThrottler {
	return &LoginThrottler{
		cfg:      cfg.withDefaults(),
		failures: map[string][]time.Time{},
		locked:   map[string]time.Time{},
		now:      time.Now,
	}
}

type throttleKey struct {
	key   string
	limit int
}

func (t *LoginThrottler) keys(a port.LoginAttempt) []throttleKey {
	keys := []throttleKey{{"e:" + a.Email, t.cfg.MaxAttempts}}
	if a.IPAddress != "" {
		keys = append(keys, throttleKey{"i:" + a.IPAddress, t.cfg.IPMaxAttempts})
	}
	return keys
}

// Allow returns core.ErrTooManyAttempts while the email or the IP is locked out.
func (t *LoginThrottler) Allow(_ context.Context, a port.LoginAttempt) error {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range t.keys(a) {
		if until, ok := t.locked[k.key]; ok && now.Before(until) {
			return core.ErrTooManyAttempts
		}
	}
	return nil
}

// Failure counts a failed attempt and locks the key once its limit is reached.
func (t *LoginThrottler) Failure(_ context.Context, a port.LoginAttempt) error {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(now)
	for _, k := range t.keys(a) {
		recent := t.recent(k.key, now)
		recent = append(recent, now)
		if len(recent) >= k.limit {
			t.locked[k.key] = now.Add(t.cfg.Lockout)
			delete(t.failures, k.key)
			continue
		}
		t.failures[k.key] = recent
	}
	return nil
}

// Success clears the failures of the email; the IP count is kept.
func (t *LoginThrottler) Success(_ context.Context, a port.LoginAttempt) error {
	t.mu.Lock()
	delete(t.failures, "e:"+a.Email)
	t.mu.Unlock()
	return nil
}

// recent returns the failures of key still inside the window.
func (t *LoginThrottler) recent(key string, now time.Time) []time.Time {
	from := now.Add(-t.cfg.Window)
	var out []time.Time
	for _, at := range t.failures[key] {
		if at.After(from) {
			out = append(out, at)
		}
	}
	return out
}

func (t *LoginThrottler) sweep(now time.Time) {
	if len(t.failures)+len(t.locked) < sweepAt {
		return
	}
	for k, until := range t.locked {
		if !now.Before(until) {
			delete(t.locked, k)
		}
	}
	for k := range t.failures {
		if len(t.recent(k, now)) == 0 {
			delete(t.failures, k)
		}
	}
}

// TicketStore implements port.TicketStore in memory. Single instance only.
type TicketStore struct {
	mu   sync.Mutex
	seen map[string]time.Time // jti -> expiry
	now  func() time.Time
}

// NewTicketStore builds an in-memory TicketStore.
func NewTicketStore() *TicketStore {
	return &TicketStore{seen: map[string]time.Time{}, now: time.Now}
}

// Consume records jti for ttl and reports false when it was already consumed.
func (s *TicketStore) Consume(_ context.Context, jti string, ttl time.Duration) (bool, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) >= sweepAt {
		for k, exp := range s.seen {
			if !now.Before(exp) {
				delete(s.seen, k)
			}
		}
	}
	if exp, ok := s.seen[jti]; ok && now.Before(exp) {
		return false, nil
	}
	s.seen[jti] = now.Add(ttl)
	return true, nil
}

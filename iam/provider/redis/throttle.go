package redis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	goredis "github.com/redis/go-redis/v9"
)

// ThrottleConfig configures a LoginThrottler. Zero values use the defaults.
type ThrottleConfig struct {
	MaxAttempts   int           // failures per email within Window; default 5
	IPMaxAttempts int           // failures per IP within Window; default 4x MaxAttempts
	Window        time.Duration // sliding window; default 15 min
	Lockout       time.Duration // lockout once a limit is reached; default 15 min
	KeyPrefix     string        // default "iam:throttle:"
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
	if c.KeyPrefix == "" {
		c.KeyPrefix = "iam:throttle:"
	}
	return c
}

// LoginThrottler implements port.LoginThrottler with Redis: a sliding window
// (sorted set of failure times) per email and per IP, and a lockout key set
// once a limit is reached. Shared by every instance.
type LoginThrottler struct {
	client goredis.UniversalClient
	cfg    ThrottleConfig
}

// NewLoginThrottler builds a Redis LoginThrottler.
func NewLoginThrottler(client goredis.UniversalClient, cfg ThrottleConfig) *LoginThrottler {
	return &LoginThrottler{client: client, cfg: cfg.withDefaults()}
}

// recordFailure trims the window, adds the failure and locks the key once the
// limit is reached. KEYS: window set, lock. ARGV: now ms, window ms, limit, lockout ms, member.
var recordFailure = goredis.NewScript(`
local now = tonumber(ARGV[1])
redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now - tonumber(ARGV[2]))
redis.call("ZADD", KEYS[1], now, ARGV[5])
redis.call("PEXPIRE", KEYS[1], ARGV[2])
if redis.call("ZCARD", KEYS[1]) >= tonumber(ARGV[3]) then
	redis.call("SET", KEYS[2], "1", "PX", ARGV[4])
	redis.call("DEL", KEYS[1])
end
return 0`)

type throttleKey struct {
	window, lock string
	limit        int
}

// keys hashes the email and IP (no PII in Redis); the hash tag keeps both
// keys of a dimension in one cluster slot for the script.
func (t *LoginThrottler) keys(a port.LoginAttempt) []throttleKey {
	mk := func(kind, v string, limit int) throttleKey {
		sum := sha256.Sum256([]byte(v))
		base := t.cfg.KeyPrefix + "{" + kind + ":" + hex.EncodeToString(sum[:16]) + "}"
		return throttleKey{window: base + ":f", lock: base + ":l", limit: limit}
	}
	keys := []throttleKey{mk("e", a.Email, t.cfg.MaxAttempts)}
	if a.IPAddress != "" {
		keys = append(keys, mk("i", a.IPAddress, t.cfg.IPMaxAttempts))
	}
	return keys
}

// Allow returns core.ErrTooManyAttempts while the email or the IP is locked out.
func (t *LoginThrottler) Allow(ctx context.Context, a port.LoginAttempt) error {
	for _, k := range t.keys(a) {
		n, err := t.client.Exists(ctx, k.lock).Result()
		if err != nil {
			return fmt.Errorf("iam/redis: throttle check: %w", err)
		}
		if n > 0 {
			return core.ErrTooManyAttempts
		}
	}
	return nil
}

// Failure counts a failed attempt for the email and the IP.
func (t *LoginThrottler) Failure(ctx context.Context, a port.LoginAttempt) error {
	now := time.Now().UnixMilli()
	for _, k := range t.keys(a) {
		err := recordFailure.Run(ctx, t.client, []string{k.window, k.lock},
			now, t.cfg.Window.Milliseconds(), k.limit, t.cfg.Lockout.Milliseconds(), rand.Text()).Err()
		if err != nil {
			return fmt.Errorf("iam/redis: throttle record: %w", err)
		}
	}
	return nil
}

// Success clears the failures of the email; the IP count and any lockout are kept.
func (t *LoginThrottler) Success(ctx context.Context, a port.LoginAttempt) error {
	if err := t.client.Del(ctx, t.keys(a)[0].window).Err(); err != nil {
		return fmt.Errorf("iam/redis: throttle reset: %w", err)
	}
	return nil
}

// TicketStore implements port.TicketStore with SET NX, shared by every instance.
type TicketStore struct {
	client    goredis.UniversalClient
	keyPrefix string
}

// NewTicketStore builds a Redis TicketStore; keyPrefix defaults to "iam:ticket:".
func NewTicketStore(client goredis.UniversalClient, keyPrefix string) *TicketStore {
	if keyPrefix == "" {
		keyPrefix = "iam:ticket:"
	}
	return &TicketStore{client: client, keyPrefix: keyPrefix}
}

// Consume records jti for ttl and reports false when it was already consumed.
func (s *TicketStore) Consume(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	ok, err := s.client.SetNX(ctx, s.keyPrefix+jti, 1, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("iam/redis: consume ticket: %w", err)
	}
	return ok, nil
}

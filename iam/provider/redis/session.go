// Package redis implements port.SessionPort using Redis.
// Recommended for production: distributed, native TTL, and instant cross-instance revocation.
package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	goredis "github.com/redis/go-redis/v9"
)

// Config configures the Redis session provider.
type Config struct {
	// Standalone mode.
	Addr     string
	Password string
	DB       int

	// Cluster mode.
	ClusterAddrs []string

	KeyPrefix string // default: "iam:session:"

	TLSEnabled bool

	// Connection pool settings.
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Provider implements port.SessionPort with Redis.
type Provider struct {
	client    goredis.UniversalClient
	keyPrefix string

	beforeRevokeCommit func() // test hook: runs inside the WATCH window
}

// NewProvider builds a Redis Provider with the given configuration.
func NewProvider(cfg Config) *Provider {
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "iam:session:"
	}

	var client goredis.UniversalClient
	if len(cfg.ClusterAddrs) > 0 {
		client = goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs:        cfg.ClusterAddrs,
			Password:     cfg.Password,
			TLSConfig:    tlsConfig(cfg.TLSEnabled),
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		})
	} else {
		client = goredis.NewClient(&goredis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			TLSConfig:    tlsConfig(cfg.TLSEnabled),
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		})
	}

	return &Provider{
		client:    client,
		keyPrefix: cfg.KeyPrefix,
	}
}

// NewProviderWithClient builds a Provider using an existing Redis client.
// Useful for tests using miniredis or to reuse an existing connection.
func NewProviderWithClient(client goredis.UniversalClient, keyPrefix string) *Provider {
	if keyPrefix == "" {
		keyPrefix = "iam:session:"
	}
	return &Provider{client: client, keyPrefix: keyPrefix}
}

// Save persists the session in Redis with a TTL calculated from ExpiresAt.
// The raw RefreshToken field is never serialized — only RefreshTokenHash is persisted.
// SessionExtra is stored as is (plain JSON): protect Redis with TLS and ACLs
// when it carries external provider tokens.
func (p *Provider) Save(ctx context.Context, session *types.Session) error {
	// Copy without the raw RefreshToken to prevent accidental persistence.
	safe := sanitize(session)

	data, err := json.Marshal(safe) // #nosec G117 -- sanitize clears AccessToken and RefreshToken
	if err != nil {
		return fmt.Errorf("iam/redis: failed to marshal session: %w", err)
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("iam/redis: session already expired")
	}

	// Pipelined (not MULTI): the session and user index keys may live in different cluster slots.
	userKey := p.userKey(session.UserID)
	_, err = p.client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
		pipe.Set(ctx, p.sessionKey(session.ID), data, ttl)
		// User index to support ListByUser and RevokeAllForUser.
		pipe.SAdd(ctx, userKey, session.ID)
		pipe.Expire(ctx, userKey, ttl+time.Minute)
		return nil
	})
	if err != nil {
		return fmt.Errorf("iam/redis: failed to save session: %w", err)
	}
	return nil
}

// Get retrieves a session by ID.
func (p *Provider) Get(ctx context.Context, sessionID string) (*types.Session, error) {
	data, err := p.client.Get(ctx, p.sessionKey(sessionID)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, core.ErrSessionNotFound
		}
		return nil, fmt.Errorf("iam/redis: failed to get session: %w", err)
	}

	var session types.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("iam/redis: failed to unmarshal session: %w", err)
	}

	return &session, nil
}

// Revoke invalidates a specific session by marking it as revoked and updating it in Redis.
func (p *Provider) Revoke(ctx context.Context, sessionID string) error {
	session, err := p.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	data, err := json.Marshal(markRevoked(session)) // #nosec G117 -- loaded sessions carry no raw tokens
	if err != nil {
		return fmt.Errorf("iam/redis: failed to marshal revoked session: %w", err)
	}

	return p.client.Set(ctx, p.sessionKey(sessionID), data, revokedTTL(session)).Err()
}

// RevokeIfActive revokes the session with an optimistic WATCH/MULTI transaction,
// so only one of several concurrent refreshes can rotate it.
func (p *Provider) RevokeIfActive(ctx context.Context, sessionID string) (bool, error) {
	key := p.sessionKey(sessionID)
	revoked := false
	err := p.client.Watch(ctx, func(tx *goredis.Tx) error {
		data, err := tx.Get(ctx, key).Bytes()
		if errors.Is(err, goredis.Nil) {
			return core.ErrSessionNotFound
		}
		if err != nil {
			return fmt.Errorf("iam/redis: failed to get session: %w", err)
		}
		var s types.Session
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("iam/redis: failed to unmarshal session: %w", err)
		}
		if s.Revoked {
			return nil
		}
		ttl := revokedTTL(&s)
		out, err := json.Marshal(markRevoked(&s)) // #nosec G117 -- loaded sessions carry no raw tokens
		if err != nil {
			return fmt.Errorf("iam/redis: failed to marshal revoked session: %w", err)
		}
		if p.beforeRevokeCommit != nil {
			p.beforeRevokeCommit()
		}
		_, err = tx.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
			pipe.Set(ctx, key, out, ttl)
			return nil
		})
		revoked = err == nil
		return err
	}, key)
	if errors.Is(err, goredis.TxFailedErr) {
		return false, nil // a concurrent writer changed the session first
	}
	return revoked, err
}

// revokedBeforeTTL outlives any session (SessionMaxLifetime is at most 90 days).
const revokedBeforeTTL = 91 * 24 * time.Hour

// RevokeAllForUser first records the user's cut-off (see RevokedBefore), which
// also rejects sessions a concurrent refresh opens, then revokes every indexed
// session. Every failure is returned (joined); expired ids leave the index.
func (p *Provider) RevokeAllForUser(ctx context.Context, userID string) error {
	now := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := p.client.Set(ctx, p.revokedBeforeKey(userID), now, revokedBeforeTTL).Err(); err != nil {
		return fmt.Errorf("iam/redis: failed to record revocation cut-off: %w", err)
	}
	sessionIDs, err := p.client.SMembers(ctx, p.userKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("iam/redis: failed to list user sessions: %w", err)
	}

	var errs []error
	var stale []string
	for _, id := range sessionIDs {
		switch err := p.Revoke(ctx, id); {
		case err == nil:
			stale = append(stale, id) // revoked: no longer listed
		case errors.Is(err, core.ErrSessionNotFound):
			stale = append(stale, id)
		default:
			errs = append(errs, fmt.Errorf("iam/redis: revoke session %s: %w", id, err))
		}
	}
	errs = append(errs, p.prune(ctx, userID, stale))
	return errors.Join(errs...)
}

// RevokedBefore implements port.UserRevocationStore; zero when never set.
func (p *Provider) RevokedBefore(ctx context.Context, userID string) (time.Time, error) {
	ns, err := p.client.Get(ctx, p.revokedBeforeKey(userID)).Int64()
	if errors.Is(err, goredis.Nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("iam/redis: failed to read revocation cut-off: %w", err)
	}
	return time.Unix(0, ns), nil
}

// ListByUser returns the active sessions for the given user; expired and
// revoked ids are pruned from the user index on the way.
func (p *Provider) ListByUser(ctx context.Context, userID string) ([]*types.Session, error) {
	sessionIDs, err := p.client.SMembers(ctx, p.userKey(userID)).Result()
	if err != nil {
		return nil, fmt.Errorf("iam/redis: failed to list user sessions: %w", err)
	}

	var sessions []*types.Session
	var stale []string
	for _, id := range sessionIDs {
		s, err := p.Get(ctx, id)
		switch {
		case errors.Is(err, core.ErrSessionNotFound):
			stale = append(stale, id)
		case err != nil:
			return nil, err
		case s.Revoked:
			stale = append(stale, id)
		default:
			sessions = append(sessions, s)
		}
	}
	if err := p.prune(ctx, userID, stale); err != nil {
		return nil, err
	}
	return sessions, nil
}

// prune removes ids from the user index. Revoked sessions stay readable by id
// (refresh token reuse detection) but are no longer listed.
func (p *Provider) prune(ctx context.Context, userID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	members := make([]any, len(ids))
	for i, id := range ids {
		members[i] = id
	}
	if err := p.client.SRem(ctx, p.userKey(userID), members...).Err(); err != nil {
		return fmt.Errorf("iam/redis: failed to prune user index: %w", err)
	}
	return nil
}

func (p *Provider) sessionKey(sessionID string) string {
	return p.keyPrefix + sessionID
}

func (p *Provider) userKey(userID string) string {
	return p.keyPrefix + "user:" + userID
}

// revokedBeforeKey lives outside the "user:" namespace so no user id can
// collide with another user's index key.
func (p *Provider) revokedBeforeKey(userID string) string {
	return p.keyPrefix + "revoked_before:" + userID
}

// Client returns the underlying Redis client, to share it with the login
// throttler and the ticket store.
func (p *Provider) Client() goredis.UniversalClient { return p.client }

// revokedRetention is the minimum time a revoked session is kept.
const revokedRetention = 5 * time.Minute

// revokedTTL keeps a revoked session (with its token hash) until it would
// have expired, so refresh token reuse is detected for the token's whole life.
func revokedTTL(s *types.Session) time.Duration {
	return max(time.Until(s.ExpiresAt), revokedRetention)
}

// markRevoked keeps the original revocation time of an already revoked session.
func markRevoked(s *types.Session) *types.Session {
	if s.Revoked {
		return sanitize(s)
	}
	now := time.Now()
	s.Revoked = true
	s.RevokedAt = &now
	s.RevokedBy = "user"
	return sanitize(s)
}

// sanitize returns a copy of the session with the raw RefreshToken cleared.
func sanitize(s *types.Session) *types.Session {
	copy := *s
	copy.RefreshToken = "" // never persist the raw token
	copy.AccessToken = ""  // a leaked store must not yield usable bearer tokens
	return &copy
}

// tlsConfig returns a TLS 1.2+ config when enabled; the server name is taken from each dial address.
func tlsConfig(enabled bool) *tls.Config {
	if !enabled {
		return nil
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

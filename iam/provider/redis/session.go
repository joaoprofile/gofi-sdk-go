// Package redis implements port.SessionPort using Redis.
// Recommended for production: distributed, native TTL, and instant cross-instance revocation.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
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
func (p *Provider) Save(ctx context.Context, session *types.Session) error {
	// Copy without the raw RefreshToken to prevent accidental persistence.
	safe := sanitize(session)

	data, err := json.Marshal(safe)
	if err != nil {
		return fmt.Errorf("iam/redis: failed to marshal session: %w", err)
	}

	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("iam/redis: session already expired")
	}

	key := p.sessionKey(session.ID)
	if err := p.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("iam/redis: failed to save session: %w", err)
	}

	// User index to support ListByUser and RevokeAllForUser.
	userKey := p.userKey(session.UserID)
	p.client.SAdd(ctx, userKey, session.ID)        //nolint:errcheck
	p.client.Expire(ctx, userKey, ttl+time.Minute) //nolint:errcheck

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

	now := time.Now()
	revokedBy := "user"
	session.Revoked = true
	session.RevokedAt = &now
	session.RevokedBy = revokedBy

	// Keeps the session for 5 more minutes after revocation to detect refresh token reuse.
	data, err := json.Marshal(sanitize(session))
	if err != nil {
		return fmt.Errorf("iam/redis: failed to marshal revoked session: %w", err)
	}

	return p.client.Set(ctx, p.sessionKey(sessionID), data, 5*time.Minute).Err()
}

// RevokeAllForUser invalidates all sessions for the given user.
func (p *Provider) RevokeAllForUser(ctx context.Context, userID string) error {
	userKey := p.userKey(userID)
	sessionIDs, err := p.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return fmt.Errorf("iam/redis: failed to list user sessions: %w", err)
	}

	for _, id := range sessionIDs {
		// Individual errors do not interrupt the loop — best-effort for all sessions.
		p.Revoke(ctx, id) //nolint:errcheck
	}

	return nil
}

// ListByUser returns the active sessions for the given user.
func (p *Provider) ListByUser(ctx context.Context, userID string) ([]*types.Session, error) {
	userKey := p.userKey(userID)
	sessionIDs, err := p.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return nil, fmt.Errorf("iam/redis: failed to list user sessions: %w", err)
	}

	var sessions []*types.Session
	for _, id := range sessionIDs {
		s, err := p.Get(ctx, id)
		if err != nil {
			continue // session expired or removed
		}
		if !s.Revoked {
			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}

func (p *Provider) sessionKey(sessionID string) string {
	return p.keyPrefix + sessionID
}

func (p *Provider) userKey(userID string) string {
	return p.keyPrefix + "user:" + userID
}

// sanitize returns a copy of the session with the raw RefreshToken cleared.
func sanitize(s *types.Session) *types.Session {
	copy := *s
	copy.RefreshToken = "" // never persist the raw token
	return &copy
}

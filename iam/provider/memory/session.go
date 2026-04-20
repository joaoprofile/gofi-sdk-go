// Package memory implements port.SessionPort in memory.
// Two modes are supported: with TTL for development and CI with periodic GC,
// and without TTL for unit tests with deterministic, goroutine-free behavior.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
)

// Provider implements port.SessionPort in memory.
type Provider struct {
	mu       sync.RWMutex
	sessions map[string]*types.Session
	withTTL  bool
	stopGC   chan struct{}
}

// NewProvider creates an in-memory Provider with TTL and periodic GC.
// Suitable for development and ephemeral environments.
// Not suitable for multiple instances as it is not distributed.
func NewProvider() *Provider {
	p := &Provider{
		sessions: make(map[string]*types.Session),
		withTTL:  true,
		stopGC:   make(chan struct{}),
	}
	go p.runGC(30 * time.Second)
	return p
}

// NewTestProvider creates an in-memory Provider without TTL and without goroutines.
// Intended exclusively for unit tests.
func NewTestProvider() *Provider {
	return &Provider{
		sessions: make(map[string]*types.Session),
		withTTL:  false,
	}
}

// Save persists the session in memory. The raw RefreshToken is never stored.
func (p *Provider) Save(_ context.Context, session *types.Session) error {
	copy := *session
	copy.RefreshToken = "" // never store the raw token

	p.mu.Lock()
	p.sessions[session.ID] = &copy
	p.mu.Unlock()
	return nil
}

// Get retrieves a session by ID. Checks TTL if the provider was created with TTL enabled.
func (p *Provider) Get(_ context.Context, sessionID string) (*types.Session, error) {
	p.mu.RLock()
	s, ok := p.sessions[sessionID]
	p.mu.RUnlock()

	if !ok {
		return nil, core.ErrSessionNotFound
	}

	if p.withTTL && time.Now().After(s.ExpiresAt) && !s.Revoked {
		p.mu.Lock()
		delete(p.sessions, sessionID)
		p.mu.Unlock()
		return nil, core.ErrSessionNotFound
	}

	// Returns a copy for immutability.
	copy := *s
	return &copy, nil
}

// Revoke invalidates a session by marking it as revoked.
func (p *Provider) Revoke(_ context.Context, sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	s, ok := p.sessions[sessionID]
	if !ok {
		return core.ErrSessionNotFound
	}

	now := time.Now()
	s.Revoked = true
	s.RevokedAt = &now
	s.RevokedBy = "user"
	return nil
}

// RevokeAllForUser invalidates all sessions for the given user.
func (p *Provider) RevokeAllForUser(_ context.Context, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for _, s := range p.sessions {
		if s.UserID == userID && !s.Revoked {
			s.Revoked = true
			s.RevokedAt = &now
			s.RevokedBy = "system"
		}
	}
	return nil
}

// ListByUser returns the active sessions for the given user.
func (p *Provider) ListByUser(_ context.Context, userID string) ([]*types.Session, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*types.Session
	now := time.Now()
	for _, s := range p.sessions {
		if s.UserID != userID || s.Revoked {
			continue
		}
		if p.withTTL && now.After(s.ExpiresAt) {
			continue
		}
		copy := *s
		result = append(result, &copy)
	}
	return result, nil
}

// Stop terminates the GC goroutine. Call in a defer when using NewProvider in integration tests.
func (p *Provider) Stop() {
	if p.withTTL && p.stopGC != nil {
		close(p.stopGC)
	}
}

// runGC removes expired sessions periodically.
func (p *Provider) runGC(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.gc()
		case <-p.stopGC:
			return
		}
	}
}

// gc removes sessions that have been expired for more than 5 minutes.
// The 5-minute grace period allows detection of refresh token reuse after revocation.
func (p *Provider) gc() {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, s := range p.sessions {
		if now.After(s.ExpiresAt.Add(5 * time.Minute)) {
			delete(p.sessions, id)
		}
	}
}

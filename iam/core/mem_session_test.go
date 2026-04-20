package core

import (
	"context"
	"sync"
	"time"

	"github.com/joaoprofile/gofi/iam/types"
)

// memSession is a minimal in-memory SessionPort for use in core package tests.
// It avoids the import cycle that would occur if provider/memory were imported here.
type memSession struct {
	mu       sync.RWMutex
	sessions map[string]*types.Session
}

func newMemSession() *memSession {
	return &memSession{sessions: make(map[string]*types.Session)}
}

func (m *memSession) Save(_ context.Context, s *types.Session) error {
	cp := *s
	cp.RefreshToken = ""
	m.mu.Lock()
	m.sessions[s.ID] = &cp
	m.mu.Unlock()
	return nil
}

func (m *memSession) Get(_ context.Context, id string) (*types.Session, error) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *memSession) Revoke(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	now := time.Now()
	s.Revoked = true
	s.RevokedAt = &now
	s.RevokedBy = "user"
	return nil
}

func (m *memSession) RevokeAllForUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		if s.UserID == userID && !s.Revoked {
			s.Revoked = true
			s.RevokedAt = &now
			s.RevokedBy = "system"
		}
	}
	return nil
}

func (m *memSession) ListByUser(_ context.Context, userID string) ([]*types.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*types.Session
	for _, s := range m.sessions {
		if s.UserID == userID && !s.Revoked {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

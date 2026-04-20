package memory

import (
	"context"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSession(id, userID string, expiresIn time.Duration) *types.Session {
	return &types.Session{
		ID:           id,
		UserID:       userID,
		RefreshToken: "raw-refresh-token",
		ExpiresAt:    time.Now().Add(expiresIn),
		CreatedAt:    time.Now(),
	}
}

func TestTestProvider_Save_Success(t *testing.T) {
	p := NewTestProvider()
	sess := newSession("s1", "u1", time.Hour)
	err := p.Save(context.Background(), sess)
	assert.NoError(t, err)
}

func TestTestProvider_Save_DoesNotStoreRawRefreshToken(t *testing.T) {
	p := NewTestProvider()
	sess := newSession("s1", "u1", time.Hour)
	_ = p.Save(context.Background(), sess)

	stored, err := p.Get(context.Background(), "s1")
	require.NoError(t, err)
	assert.Empty(t, stored.RefreshToken)
}

func TestTestProvider_Get_Found(t *testing.T) {
	p := NewTestProvider()
	sess := newSession("s1", "u1", time.Hour)
	_ = p.Save(context.Background(), sess)

	stored, err := p.Get(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stored.ID)
	assert.Equal(t, "u1", stored.UserID)
}

func TestTestProvider_Get_NotFound(t *testing.T) {
	p := NewTestProvider()
	_, err := p.Get(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, core.ErrSessionNotFound)
}

func TestTestProvider_Get_ReturnsCopy(t *testing.T) {
	p := NewTestProvider()
	sess := newSession("s1", "u1", time.Hour)
	_ = p.Save(context.Background(), sess)

	s1, _ := p.Get(context.Background(), "s1")
	s2, _ := p.Get(context.Background(), "s1")
	assert.Equal(t, s1.ID, s2.ID)
	// Modifying one copy should not affect the other.
	s1.UserID = "modified"
	assert.NotEqual(t, s1.UserID, s2.UserID)
}

func TestTestProvider_Get_ExpiredSessionWithoutTTL_StillReturned(t *testing.T) {
	// Without TTL (TestProvider), expired sessions are not evicted automatically.
	p := NewTestProvider()
	sess := newSession("s1", "u1", -time.Second) // already expired
	_ = p.Save(context.Background(), sess)

	stored, err := p.Get(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", stored.ID)
}

func TestNewProvider_Get_ExpiredSessionWithTTL_NotFound(t *testing.T) {
	p := NewTestProvider()
	p.withTTL = true // enable TTL check without starting GC goroutine
	sess := newSession("s1", "u1", -time.Second)
	_ = p.Save(context.Background(), sess)

	_, err := p.Get(context.Background(), "s1")
	assert.ErrorIs(t, err, core.ErrSessionNotFound)
}

func TestTestProvider_Revoke_Success(t *testing.T) {
	p := NewTestProvider()
	sess := newSession("s1", "u1", time.Hour)
	_ = p.Save(context.Background(), sess)

	err := p.Revoke(context.Background(), "s1")
	require.NoError(t, err)

	stored, err := p.Get(context.Background(), "s1")
	require.NoError(t, err)
	assert.True(t, stored.Revoked)
	assert.NotNil(t, stored.RevokedAt)
	assert.Equal(t, "user", stored.RevokedBy)
}

func TestTestProvider_Revoke_NotFound(t *testing.T) {
	p := NewTestProvider()
	err := p.Revoke(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, core.ErrSessionNotFound)
}

func TestTestProvider_RevokeAllForUser_RevokesAllUserSessions(t *testing.T) {
	p := NewTestProvider()
	_ = p.Save(context.Background(), newSession("s1", "u1", time.Hour))
	_ = p.Save(context.Background(), newSession("s2", "u1", time.Hour))
	_ = p.Save(context.Background(), newSession("s3", "u2", time.Hour)) // different user

	err := p.RevokeAllForUser(context.Background(), "u1")
	require.NoError(t, err)

	s1, _ := p.Get(context.Background(), "s1")
	s2, _ := p.Get(context.Background(), "s2")
	s3, _ := p.Get(context.Background(), "s3")

	assert.True(t, s1.Revoked)
	assert.True(t, s2.Revoked)
	assert.False(t, s3.Revoked)
}

func TestTestProvider_RevokeAllForUser_SetsRevokedBySystem(t *testing.T) {
	p := NewTestProvider()
	_ = p.Save(context.Background(), newSession("s1", "u1", time.Hour))

	_ = p.RevokeAllForUser(context.Background(), "u1")

	stored, _ := p.Get(context.Background(), "s1")
	assert.Equal(t, "system", stored.RevokedBy)
}

func TestTestProvider_RevokeAllForUser_DoesNotRevokeAlreadyRevoked(t *testing.T) {
	p := NewTestProvider()
	_ = p.Save(context.Background(), newSession("s1", "u1", time.Hour))
	_ = p.Revoke(context.Background(), "s1") // already revoked as "user"

	_ = p.RevokeAllForUser(context.Background(), "u1")

	stored, _ := p.Get(context.Background(), "s1")
	// RevokedBy should remain "user", not overwritten by "system".
	assert.Equal(t, "user", stored.RevokedBy)
}

func TestTestProvider_ListByUser_ReturnsOnlyActiveSessions(t *testing.T) {
	p := NewTestProvider()
	_ = p.Save(context.Background(), newSession("s1", "u1", time.Hour))
	_ = p.Save(context.Background(), newSession("s2", "u1", time.Hour))
	_ = p.Save(context.Background(), newSession("s3", "u2", time.Hour)) // different user
	_ = p.Revoke(context.Background(), "s2")                            // revoked

	sessions, err := p.ListByUser(context.Background(), "u1")
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

func TestTestProvider_ListByUser_EmptyWhenNoSessions(t *testing.T) {
	p := NewTestProvider()
	sessions, err := p.ListByUser(context.Background(), "u1")
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestTestProvider_ListByUser_TTL_ExcludesExpired(t *testing.T) {
	p := NewTestProvider()
	p.withTTL = true
	_ = p.Save(context.Background(), newSession("s1", "u1", time.Hour))
	_ = p.Save(context.Background(), newSession("s2", "u1", -time.Second)) // expired

	sessions, err := p.ListByUser(context.Background(), "u1")
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
	assert.Equal(t, "s1", sessions[0].ID)
}

func TestProvider_Stop_DoesNotPanic(t *testing.T) {
	p := NewProvider()
	assert.NotPanics(t, func() { p.Stop() })
}

func TestTestProvider_Stop_NoopWithoutTTL(t *testing.T) {
	p := NewTestProvider()
	assert.NotPanics(t, func() { p.Stop() })
}

func TestProvider_GC_RemovesExpiredSessions(t *testing.T) {
	p := NewTestProvider()
	// Manually add a session that expired more than 5 minutes ago.
	expired := &types.Session{
		ID:        "old-session",
		UserID:    "u1",
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	}
	p.sessions["old-session"] = expired

	p.gc()

	_, err := p.Get(context.Background(), "old-session")
	assert.ErrorIs(t, err, core.ErrSessionNotFound)
}

func TestProvider_GC_KeepsRecentlyExpiredSessions(t *testing.T) {
	p := NewTestProvider()
	// Session expired 2 minutes ago — within the 5-minute grace window.
	recent := &types.Session{
		ID:        "recent-session",
		UserID:    "u1",
		ExpiresAt: time.Now().Add(-2 * time.Minute),
	}
	p.sessions["recent-session"] = recent

	p.gc()

	_, err := p.Get(context.Background(), "recent-session")
	// Session should still be there (within grace window).
	assert.NoError(t, err)
}

func TestTestProvider_ConcurrentSaveAndGet(t *testing.T) {
	p := NewTestProvider()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			_ = p.Save(context.Background(), newSession("concurrent-s", "u1", time.Hour))
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_, _ = p.Get(context.Background(), "concurrent-s")
	}
	<-done
}

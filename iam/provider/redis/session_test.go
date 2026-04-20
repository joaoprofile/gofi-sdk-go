package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/joaoprofile/gofi/iam/core"
	"github.com/joaoprofile/gofi/iam/types"
	goredis "github.com/redis/go-redis/v9"
)

// newTestProvider spins up a miniredis server and returns a Provider backed by it.
func newTestProvider(t *testing.T) (*Provider, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return NewProviderWithClient(client, "test:"), mr
}

func newTestSession(id, userID string, ttl time.Duration) *types.Session {
	now := time.Now()
	return &types.Session{
		ID:                   id,
		UserID:               userID,
		TenantID:             "tenant1",
		Module:               "mod",
		AccessToken:          "at-" + id,
		RefreshToken:         "raw-secret",
		RefreshTokenHash:     "hashed-secret",
		RefreshTokenLastFour: "ecre",
		AuthProvider:         "local",
		ExpiresAt:            now.Add(ttl),
		CreatedAt:            now,
		LastUsedAt:           now,
		IPAddress:            "127.0.0.1",
		UserAgent:            "test",
	}
}

// ---- NewProvider ----

func TestNewProvider_Standalone(t *testing.T) {
	mr := miniredis.RunT(t)
	p := NewProvider(Config{
		Addr:      mr.Addr(),
		KeyPrefix: "pfx:",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.keyPrefix != "pfx:" {
		t.Errorf("keyPrefix=%q, want pfx:", p.keyPrefix)
	}
}

func TestNewProvider_DefaultKeyPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	p := NewProvider(Config{Addr: mr.Addr()})
	if p.keyPrefix != "iam:session:" {
		t.Errorf("keyPrefix=%q, want iam:session:", p.keyPrefix)
	}
}

func TestNewProvider_Cluster(t *testing.T) {
	// Just verify it doesn't panic with cluster addresses — we don't start a real cluster.
	p := NewProvider(Config{
		ClusterAddrs: []string{"localhost:7000", "localhost:7001"},
		KeyPrefix:    "cluster:",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewProviderWithClient_DefaultPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	p := NewProviderWithClient(client, "")
	if p.keyPrefix != "iam:session:" {
		t.Errorf("keyPrefix=%q, want iam:session:", p.keyPrefix)
	}
}

// ---- Save ----

func TestSave_Success(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("s1", "u1", time.Hour)

	if err := p.Save(ctx, sess); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}
}

func TestSave_ExpiredSession(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("s2", "u1", -time.Second)

	err := p.Save(ctx, sess)
	if err == nil {
		t.Fatal("expected error for expired session, got nil")
	}
}

func TestSave_DoesNotPersistRawToken(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("s3", "u1", time.Hour)

	if err := p.Save(ctx, sess); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}

	// Inspect raw value stored in miniredis.
	raw, err := mr.Get("test:s3")
	if err != nil {
		t.Fatalf("mr.Get() error: %v", err)
	}
	if containsStr(raw, "raw-secret") {
		t.Error("raw RefreshToken must not be persisted in Redis")
	}
	if !containsStr(raw, "hashed-secret") {
		t.Error("RefreshTokenHash must be persisted in Redis")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && searchStr(s, sub))
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---- Get ----

func TestGet_Success(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("s4", "u1", time.Hour)

	if err := p.Save(ctx, sess); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := p.Get(ctx, "s4")
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got.ID != "s4" {
		t.Errorf("ID=%q, want s4", got.ID)
	}
	if got.RefreshToken != "" {
		t.Error("RefreshToken must be empty — was sanitized before save")
	}
}

func TestGet_NotFound(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	_, err := p.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected ErrSessionNotFound, got nil")
	}
	if err != core.ErrSessionNotFound {
		t.Errorf("expected core.ErrSessionNotFound, got %v", err)
	}
}

func TestGet_UnmarshalError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()

	// Write invalid JSON for a key.
	mr.Set("test:badkey", "not-json")

	_, err := p.Get(ctx, "badkey")
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

// ---- Revoke ----

func TestRevoke_Success(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("s5", "u1", time.Hour)

	if err := p.Save(ctx, sess); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := p.Revoke(ctx, "s5"); err != nil {
		t.Fatalf("Revoke() unexpected error: %v", err)
	}

	got, err := p.Get(ctx, "s5")
	if err != nil {
		t.Fatalf("Get() after Revoke() error: %v", err)
	}
	if !got.Revoked {
		t.Error("expected session to be Revoked=true")
	}
	if got.RevokedAt == nil {
		t.Error("expected RevokedAt to be set")
	}
	if got.RevokedBy != "user" {
		t.Errorf("RevokedBy=%q, want user", got.RevokedBy)
	}
}

func TestRevoke_SessionNotFound(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	err := p.Revoke(ctx, "no-such-session")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

// ---- RevokeAllForUser ----

func TestRevokeAllForUser_Success(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	// Save two sessions for the same user.
	s1 := newTestSession("sa1", "userA", time.Hour)
	s2 := newTestSession("sa2", "userA", time.Hour)
	if err := p.Save(ctx, s1); err != nil {
		t.Fatalf("Save(s1) error: %v", err)
	}
	if err := p.Save(ctx, s2); err != nil {
		t.Fatalf("Save(s2) error: %v", err)
	}

	if err := p.RevokeAllForUser(ctx, "userA"); err != nil {
		t.Fatalf("RevokeAllForUser() unexpected error: %v", err)
	}
}

func TestRevokeAllForUser_NoSessions(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	// No sessions — must succeed silently.
	if err := p.RevokeAllForUser(ctx, "userWithNoSessions"); err != nil {
		t.Fatalf("RevokeAllForUser() unexpected error: %v", err)
	}
}

// ---- ListByUser ----

func TestListByUser_ReturnsActiveSessions(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	s1 := newTestSession("lb1", "userB", time.Hour)
	s2 := newTestSession("lb2", "userB", time.Hour)
	if err := p.Save(ctx, s1); err != nil {
		t.Fatalf("Save(s1) error: %v", err)
	}
	if err := p.Save(ctx, s2); err != nil {
		t.Fatalf("Save(s2) error: %v", err)
	}

	sessions, err := p.ListByUser(ctx, "userB")
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestListByUser_ExcludesRevoked(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	s1 := newTestSession("lc1", "userC", time.Hour)
	s2 := newTestSession("lc2", "userC", time.Hour)
	if err := p.Save(ctx, s1); err != nil {
		t.Fatalf("Save(s1) error: %v", err)
	}
	if err := p.Save(ctx, s2); err != nil {
		t.Fatalf("Save(s2) error: %v", err)
	}

	// Revoke one session.
	if err := p.Revoke(ctx, "lc1"); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}

	sessions, err := p.ListByUser(ctx, "userC")
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 active session, got %d", len(sessions))
	}
	if sessions[0].ID != "lc2" {
		t.Errorf("expected lc2, got %q", sessions[0].ID)
	}
}

func TestListByUser_Empty(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	sessions, err := p.ListByUser(ctx, "noone")
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

// ---- sanitize ----

func TestSanitize(t *testing.T) {
	now := time.Now()
	s := &types.Session{
		ID:           "sid",
		RefreshToken: "secret",
		ExpiresAt:    now.Add(time.Hour),
	}

	safe := sanitize(s)

	if safe.RefreshToken != "" {
		t.Errorf("sanitize must clear RefreshToken, got %q", safe.RefreshToken)
	}
	if safe.ID != "sid" {
		t.Errorf("sanitize must preserve ID, got %q", safe.ID)
	}
	// Original must be untouched.
	if s.RefreshToken != "secret" {
		t.Error("sanitize must not mutate the original")
	}
}

// ---- Redis error paths ----

func TestSave_RedisSetError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("err1", "u1", time.Hour)

	// Close miniredis to force a connection error on Set.
	mr.Close()

	err := p.Save(ctx, sess)
	if err == nil {
		t.Fatal("expected error when Redis is unavailable, got nil")
	}
}

func TestGet_RedisGetError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()

	// Close miniredis before get — triggers non-Nil Redis error.
	mr.Close()

	_, err := p.Get(ctx, "anykey")
	if err == nil {
		t.Fatal("expected error when Redis is unavailable, got nil")
	}
}

func TestRevoke_SetError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	sess := newTestSession("rev-err", "u1", time.Hour)

	if err := p.Save(ctx, sess); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Close miniredis to fail the Set in Revoke.
	mr.Close()

	// Get must still work since we closed after save — but actually mr is closed
	// so this will fail on Get. That's fine — we just verify error propagation.
	err := p.Revoke(ctx, "rev-err")
	if err == nil {
		t.Fatal("expected error when Redis is unavailable, got nil")
	}
}

func TestRevokeAllForUser_SMembersError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()

	// Close miniredis to force SMembers failure.
	mr.Close()

	err := p.RevokeAllForUser(ctx, "userX")
	if err == nil {
		t.Fatal("expected error when Redis is unavailable, got nil")
	}
}

func TestListByUser_SMembersError(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()

	// Close miniredis to force SMembers failure.
	mr.Close()

	_, err := p.ListByUser(ctx, "userX")
	if err == nil {
		t.Fatal("expected error when Redis is unavailable, got nil")
	}
}

func TestListByUser_SkipsExpiredSessions(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()

	s1 := newTestSession("ld1", "userD", time.Hour)
	if err := p.Save(ctx, s1); err != nil {
		t.Fatalf("Save(s1) error: %v", err)
	}

	// Fast-forward miniredis to expire all sessions.
	mr.FastForward(2 * time.Hour)

	// ListByUser should still succeed; expired sessions are skipped.
	sessions, err := p.ListByUser(ctx, "userD")
	if err != nil {
		t.Fatalf("ListByUser() unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after expiry, got %d", len(sessions))
	}
}

// ---- key helpers ----

func TestSessionKey(t *testing.T) {
	p := &Provider{keyPrefix: "pfx:"}
	got := p.sessionKey("abc")
	if got != "pfx:abc" {
		t.Errorf("sessionKey=%q, want pfx:abc", got)
	}
}

func TestUserKey(t *testing.T) {
	p := &Provider{keyPrefix: "pfx:"}
	got := p.userKey("u1")
	if got != "pfx:user:u1" {
		t.Errorf("userKey=%q, want pfx:user:u1", got)
	}
}

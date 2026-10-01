package redis

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
)

var _ port.SessionRevoker = (*Provider)(nil)

func TestRevokeIfActive_OnlyOneWinner(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	if err := p.Save(ctx, newTestSession("s1", "u1", time.Hour)); err != nil {
		t.Fatal(err)
	}

	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			ok, err := p.RevokeIfActive(ctx, "s1")
			if err != nil {
				t.Error(err)
			}
			if ok {
				wins.Add(1)
			}
		})
	}
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("winners=%d, want exactly 1", wins.Load())
	}
	s, err := p.Get(ctx, "s1")
	if err != nil || !s.Revoked {
		t.Fatalf("session must be revoked: %v %+v", err, s)
	}
}

func TestRevokeIfActive_ConcurrentWriterWins(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	if err := p.Save(ctx, newTestSession("s1", "u1", time.Hour)); err != nil {
		t.Fatal(err)
	}
	p.beforeRevokeCommit = func() {
		p.beforeRevokeCommit = nil
		if err := p.Revoke(ctx, "s1"); err != nil {
			t.Error(err)
		}
	}

	ok, err := p.RevokeIfActive(ctx, "s1")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want false,nil when another writer revoked first", ok, err)
	}
}

func TestRevokeIfActive_AlreadyRevoked(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	_ = p.Save(ctx, newTestSession("s1", "u1", time.Hour))
	_ = p.Revoke(ctx, "s1")

	if ok, err := p.RevokeIfActive(ctx, "s1"); ok || err != nil {
		t.Fatalf("ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestRevokeIfActive_NotFound(t *testing.T) {
	p, _ := newTestProvider(t)
	if _, err := p.RevokeIfActive(context.Background(), "missing"); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("err=%v, want ErrSessionNotFound", err)
	}
}

func TestSave_ReturnsIndexErrors(t *testing.T) {
	p, mr := newTestProvider(t)
	mr.SetError("forced failure")
	if err := p.Save(context.Background(), newTestSession("s1", "u1", time.Hour)); err == nil {
		t.Fatal("expected Save to surface Redis errors")
	}
}

// Reuse detection needs the revoked record (and its hash) for the token's whole life, not 5 minutes.
func TestRevoke_KeepsRecordUntilOriginalExpiry(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		s := newTestSession(id, "u1", 24*time.Hour)
		s.RefreshTokenHash = "hash-" + id
		s.AuthTime = time.Now().Add(-time.Hour).Truncate(time.Second)
		if err := p.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Revoke(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if ok, err := p.RevokeIfActive(ctx, "s2"); err != nil || !ok {
		t.Fatalf("RevokeIfActive: %v %v", ok, err)
	}

	mr.FastForward(time.Hour)
	for _, id := range []string{"s1", "s2"} {
		s, err := p.Get(ctx, id)
		if err != nil {
			t.Fatalf("%s: revoked session dropped after an hour: %v", id, err)
		}
		if !s.Revoked || s.RefreshTokenHash != "hash-"+id || s.AuthTime.IsZero() {
			t.Fatalf("%s: revoked record lost fields: %+v", id, s)
		}
	}
	if ttl := mr.TTL(p.sessionKey("s1")); ttl < 22*time.Hour {
		t.Fatalf("ttl=%v, want until the original expiry", ttl)
	}

	mr.FastForward(24 * time.Hour)
	if _, err := p.Get(ctx, "s1"); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("revoked record must expire with the session: %v", err)
	}
}

func TestRevoke_NearExpiryKeepsMinimumRetention(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	if err := p.Save(ctx, newTestSession("s1", "u1", time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := p.Revoke(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(p.sessionKey("s1")); ttl < revokedRetention-time.Second {
		t.Fatalf("ttl=%v, want at least %v", ttl, revokedRetention)
	}
}

func TestRevoke_KeepsOriginalRevocationTime(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()
	if err := p.Save(ctx, newTestSession("s1", "u1", time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = p.Revoke(ctx, "s1")
	first, _ := p.Get(ctx, "s1")
	time.Sleep(10 * time.Millisecond)
	_ = p.RevokeAllForUser(ctx, "u1")
	second, _ := p.Get(ctx, "s1")
	if !first.RevokedAt.Equal(*second.RevokedAt) {
		t.Fatalf("RevokedAt changed: %v -> %v", first.RevokedAt, second.RevokedAt)
	}
}

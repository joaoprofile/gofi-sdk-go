package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
)

var (
	_ port.UserRevocationStore = (*Provider)(nil)
	_ port.LoginThrottler      = (*LoginThrottler)(nil)
	_ port.TicketStore         = (*TicketStore)(nil)
)

func TestRevokeAllForUser_RecordsCutoffAndPrunesIndex(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if err := p.Save(ctx, newTestSession(id, "u1", time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	mr.Del("test:s2") // expired: must leave the index

	before := time.Now()
	if err := p.RevokeAllForUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	cutoff, err := p.RevokedBefore(ctx, "u1")
	if err != nil || cutoff.Before(before) {
		t.Fatalf("cutoff=%v err=%v, want >= %v", cutoff, err, before)
	}
	if members, _ := mr.SMembers("test:user:u1"); len(members) != 0 {
		t.Fatalf("index not pruned: %v", members)
	}
	if s, err := p.Get(ctx, "s1"); err != nil || !s.Revoked {
		t.Fatalf("s1 must stay readable and revoked (reuse detection): %v %+v", err, s)
	}
}

func TestRevokedBefore_ZeroWhenNeverSet(t *testing.T) {
	p, _ := newTestProvider(t)
	got, err := p.RevokedBefore(context.Background(), "nobody")
	if err != nil || !got.IsZero() {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

// Individual failures used to be swallowed; they must be returned while the
// other sessions are still revoked.
func TestRevokeAllForUser_ReturnsErrors(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	if err := p.Save(ctx, newTestSession("good", "u1", time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(ctx, newTestSession("bad", "u1", time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := mr.Set("test:bad", "{not json"); err != nil {
		t.Fatal(err)
	}

	if err := p.RevokeAllForUser(ctx, "u1"); err == nil {
		t.Fatal("expected the corrupt session's error")
	}
	if s, _ := p.Get(ctx, "good"); s == nil || !s.Revoked {
		t.Fatal("the other sessions must still be revoked")
	}
}

func TestListByUser_PrunesRevokedAndExpired(t *testing.T) {
	p, mr := newTestProvider(t)
	ctx := context.Background()
	for _, id := range []string{"live", "revoked", "gone"} {
		if err := p.Save(ctx, newTestSession(id, "u1", time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Revoke(ctx, "revoked"); err != nil {
		t.Fatal(err)
	}
	mr.Del("test:gone")

	list, err := p.ListByUser(ctx, "u1")
	if err != nil || len(list) != 1 || list[0].ID != "live" {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if members, _ := mr.SMembers("test:user:u1"); len(members) != 1 || members[0] != "live" {
		t.Fatalf("index=%v, want [live]", members)
	}
}

func TestRevokeAllForUser_CutoffWriteFails(t *testing.T) {
	p, mr := newTestProvider(t)
	mr.Close()
	if err := p.RevokeAllForUser(context.Background(), "u1"); err == nil {
		t.Fatal("expected error when Redis is down")
	}
	if _, err := p.RevokedBefore(context.Background(), "u1"); err == nil {
		t.Fatal("expected error when Redis is down")
	}
}

func TestLoginThrottler_LocksEmailAfterMaxAttempts(t *testing.T) {
	p, mr := newTestProvider(t)
	th := NewLoginThrottler(p.Client(), ThrottleConfig{MaxAttempts: 3, Lockout: time.Minute})
	ctx := context.Background()
	at := port.LoginAttempt{Email: "a@b.c", IPAddress: "10.0.0.1"}

	for range 2 {
		if err := th.Failure(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("below the limit: %v", err)
	}
	if err := th.Failure(ctx, at); err != nil {
		t.Fatal(err)
	}
	if err := th.Allow(ctx, at); !errors.Is(err, core.ErrTooManyAttempts) {
		t.Fatalf("err=%v, want ErrTooManyAttempts", err)
	}
	// Another IP does not unlock the email.
	if err := th.Allow(ctx, port.LoginAttempt{Email: "a@b.c", IPAddress: "10.0.0.2"}); !errors.Is(err, core.ErrTooManyAttempts) {
		t.Fatalf("err=%v", err)
	}
	for _, k := range mr.Keys() {
		if k == "a@b.c" || k == "10.0.0.1" {
			t.Fatalf("raw email/IP stored as key: %v", mr.Keys())
		}
	}
	mr.FastForward(time.Minute + time.Second)
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("lockout must expire: %v", err)
	}
}

func TestLoginThrottler_LocksIPAcrossEmails(t *testing.T) {
	p, _ := newTestProvider(t)
	th := NewLoginThrottler(p.Client(), ThrottleConfig{MaxAttempts: 100, IPMaxAttempts: 3})
	ctx := context.Background()
	for _, email := range []string{"a", "b", "c"} {
		if err := th.Failure(ctx, port.LoginAttempt{Email: email, IPAddress: "10.0.0.9"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := th.Allow(ctx, port.LoginAttempt{Email: "d", IPAddress: "10.0.0.9"}); !errors.Is(err, core.ErrTooManyAttempts) {
		t.Fatalf("err=%v, want IP lockout", err)
	}
}

func TestLoginThrottler_SuccessResetsEmailWindow(t *testing.T) {
	p, _ := newTestProvider(t)
	th := NewLoginThrottler(p.Client(), ThrottleConfig{MaxAttempts: 2})
	ctx := context.Background()
	at := port.LoginAttempt{Email: "a@b.c"}
	_ = th.Failure(ctx, at)
	if err := th.Success(ctx, at); err != nil {
		t.Fatal(err)
	}
	_ = th.Failure(ctx, at)
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("success must reset the window: %v", err)
	}
}

func TestLoginThrottler_StoreErrors(t *testing.T) {
	p, mr := newTestProvider(t)
	th := NewLoginThrottler(p.Client(), ThrottleConfig{})
	mr.Close()
	ctx := context.Background()
	at := port.LoginAttempt{Email: "a"}
	if th.Allow(ctx, at) == nil || th.Failure(ctx, at) == nil || th.Success(ctx, at) == nil {
		t.Fatal("expected errors when Redis is down")
	}
}

func TestTicketStore_SingleUse(t *testing.T) {
	p, mr := newTestProvider(t)
	s := NewTicketStore(p.Client(), "")
	ctx := context.Background()
	first, err := s.Consume(ctx, "jti-1", time.Minute)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	again, err := s.Consume(ctx, "jti-1", time.Minute)
	if err != nil || again {
		t.Fatalf("reuse accepted: %v %v", again, err)
	}
	if !mr.Exists("iam:ticket:jti-1") {
		t.Fatal("default prefix not applied")
	}
	mr.Close()
	if _, err := s.Consume(ctx, "jti-2", time.Minute); err == nil {
		t.Fatal("expected error when Redis is down")
	}
}

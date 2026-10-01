package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/types"
)

var (
	_ port.LoginThrottler      = (*LoginThrottler)(nil)
	_ port.TicketStore         = (*TicketStore)(nil)
	_ port.UserRevocationStore = (*Provider)(nil)
)

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestLoginThrottler_SlidingWindowAndLockout(t *testing.T) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	th := NewLoginThrottler(ThrottleConfig{MaxAttempts: 3, Window: time.Minute, Lockout: 5 * time.Minute})
	th.now = c.now
	ctx := context.Background()
	at := port.LoginAttempt{Email: "a@b.c", IPAddress: "10.0.0.1"}

	_ = th.Failure(ctx, at)
	_ = th.Failure(ctx, at)
	c.t = c.t.Add(2 * time.Minute) // both slid out of the window
	_ = th.Failure(ctx, at)
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("old failures must slide out: %v", err)
	}
	_ = th.Failure(ctx, at)
	_ = th.Failure(ctx, at)
	if err := th.Allow(ctx, at); !errors.Is(err, core.ErrTooManyAttempts) {
		t.Fatalf("err=%v, want lockout", err)
	}
	c.t = c.t.Add(5*time.Minute + time.Second)
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("lockout must end: %v", err)
	}
}

func TestLoginThrottler_PerIPAndSuccess(t *testing.T) {
	th := NewLoginThrottler(ThrottleConfig{MaxAttempts: 2, IPMaxAttempts: 3})
	ctx := context.Background()
	for _, e := range []string{"a", "b", "c"} {
		_ = th.Failure(ctx, port.LoginAttempt{Email: e, IPAddress: "ip"})
	}
	if err := th.Allow(ctx, port.LoginAttempt{Email: "z", IPAddress: "ip"}); !errors.Is(err, core.ErrTooManyAttempts) {
		t.Fatalf("err=%v, want IP lockout", err)
	}

	at := port.LoginAttempt{Email: "s"}
	_ = th.Failure(ctx, at)
	_ = th.Success(ctx, at)
	_ = th.Failure(ctx, at)
	if err := th.Allow(ctx, at); err != nil {
		t.Fatalf("success must reset the email: %v", err)
	}
}

func TestLoginThrottler_SweepBoundsMemory(t *testing.T) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	th := NewLoginThrottler(ThrottleConfig{MaxAttempts: 1, Window: time.Minute, Lockout: time.Minute})
	th.now = c.now
	ctx := context.Background()
	for i := range sweepAt {
		_ = th.Failure(ctx, port.LoginAttempt{Email: fmt.Sprint(i)})
	}
	c.t = c.t.Add(2 * time.Minute)
	_ = th.Failure(ctx, port.LoginAttempt{Email: "x", IPAddress: "ip"})
	if n := len(th.locked) + len(th.failures); n > 3 {
		t.Fatalf("expired entries not swept: %d", n)
	}
}

func TestTicketStore_SingleUseAndExpiry(t *testing.T) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	s := NewTicketStore()
	s.now = c.now
	ctx := context.Background()
	if ok, _ := s.Consume(ctx, "j", time.Minute); !ok {
		t.Fatal("first use must pass")
	}
	if ok, _ := s.Consume(ctx, "j", time.Minute); ok {
		t.Fatal("reuse must fail")
	}
	c.t = c.t.Add(2 * time.Minute)
	for i := range sweepAt {
		_, _ = s.Consume(ctx, fmt.Sprint(i), time.Second)
	}
	c.t = c.t.Add(time.Minute)
	_, _ = s.Consume(ctx, "trigger", time.Second)
	if len(s.seen) > 2 {
		t.Fatalf("expired jtis not swept: %d", len(s.seen))
	}
}

func TestRevokeAllForUser_RecordsCutoff(t *testing.T) {
	p := NewTestProvider()
	ctx := context.Background()
	if got, _ := p.RevokedBefore(ctx, "u1"); !got.IsZero() {
		t.Fatalf("got %v, want zero", got)
	}
	_ = p.Save(ctx, &types.Session{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	before := time.Now()
	if err := p.RevokeAllForUser(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.RevokedBefore(ctx, "u1"); got.Before(before) {
		t.Fatalf("cutoff %v before %v", got, before)
	}
}

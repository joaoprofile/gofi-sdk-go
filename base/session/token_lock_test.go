package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/session"
)

func TestRedisTokenLock_OnlyOwnerReleases(t *testing.T) {
	d, mr := setupRedis(t)
	tl, ok := d.(session.TokenLocker)
	if !ok {
		t.Fatal("RedisDriver must implement TokenLocker")
	}
	ctx := context.Background()

	tokA, got, err := tl.AcquireLockToken(ctx, "job", time.Second)
	if err != nil || !got {
		t.Fatalf("A acquire: %v %v", got, err)
	}
	mr.FastForward(2 * time.Second) // A's lock expired while A was still working
	tokB, got, err := tl.AcquireLockToken(ctx, "job", time.Minute)
	if err != nil || !got {
		t.Fatalf("B acquire: %v %v", got, err)
	}

	if err := tl.ReleaseLockToken(ctx, "job", tokA); err != nil {
		t.Fatal(err)
	}
	if locked, _ := d.IsLocked(ctx, "job"); !locked {
		t.Fatal("A must not release B's lock")
	}
	if err := tl.ReleaseLockToken(ctx, "job", tokB); err != nil {
		t.Fatal(err)
	}
	if locked, _ := d.IsLocked(ctx, "job"); locked {
		t.Fatal("owner must be able to release")
	}
}

func TestWithLock_ReleasesWhenContextCanceled(t *testing.T) {
	session.ResetSingleton()
	t.Cleanup(session.ResetSingleton)
	d, _ := setupRedis(t)
	s := session.New(d, session.DefaultSessionConfig())

	ctx, cancel := context.WithCancel(context.Background())
	ok, err := s.WithLock(ctx, "report", func() error { cancel(); return nil })
	if !ok || err != nil {
		t.Fatalf("WithLock: %v %v", ok, err)
	}
	if locked, _ := d.IsLocked(context.Background(), "report"); locked {
		t.Fatal("lock must be released even when ctx was canceled inside fn")
	}
}

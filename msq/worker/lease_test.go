package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeepAliveRenewsUntilStopped(t *testing.T) {
	var n atomic.Int32
	stop := KeepAlive(context.Background(), 10*time.Millisecond, func(context.Context) { n.Add(1) })
	time.Sleep(55 * time.Millisecond)
	stop()
	got := n.Load()
	if got < 3 {
		t.Fatalf("renewed %d times, want >= 3", got)
	}
	time.Sleep(30 * time.Millisecond)
	if n.Load() != got {
		t.Fatal("renewed after stop")
	}
	stop() // idempotent
}

// A shutdown must not drop the lease of a handler that is still draining.
func TestKeepAliveOutlivesContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	stop := KeepAlive(ctx, 10*time.Millisecond, func(rctx context.Context) {
		if rctx.Err() == nil {
			n.Add(1)
		}
	})
	defer stop()
	cancel()
	time.Sleep(45 * time.Millisecond)
	if n.Load() < 2 {
		t.Fatalf("renewed %d times after cancel, want >= 2", n.Load())
	}
}

func TestKeepAliveStopWaitsForRunningRenew(t *testing.T) {
	var running, finished atomic.Bool
	stop := KeepAlive(context.Background(), 5*time.Millisecond, func(context.Context) {
		if running.Swap(true) {
			return
		}
		time.Sleep(30 * time.Millisecond)
		finished.Store(true)
	})
	for !running.Load() {
		time.Sleep(time.Millisecond)
	}
	stop()
	if !finished.Load() {
		t.Fatal("stop returned while renew was running")
	}
}

func TestRenewEvery(t *testing.T) {
	if got := RenewEvery(30 * time.Second); got != 10*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := RenewEvery(0); got != minRenewEvery {
		t.Fatalf("got %v", got)
	}
}

func TestSlotsAcquireTakesOnlyFree(t *testing.T) {
	s := NewSlots(3)
	n, err := s.Acquire(context.Background(), 10)
	if err != nil || n != 3 {
		t.Fatalf("Acquire = %d, %v; want 3", n, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := s.Acquire(ctx, 1); err == nil {
		t.Fatal("Acquire must block while every slot is taken")
	}
	s.Release(2)
	n, err = s.Acquire(context.Background(), 1)
	if err != nil || n != 1 {
		t.Fatalf("Acquire = %d, %v; want 1 (limit)", n, err)
	}
	n, _ = s.Acquire(context.Background(), 10)
	if n != 1 {
		t.Fatalf("Acquire = %d; want 1 (only one free)", n)
	}
}

func TestSlotsReleaseBeyondCapacityDoesNotBlock(t *testing.T) {
	s := NewSlots(1)
	s.Release(3)
	n, _ := s.Acquire(context.Background(), 10)
	if n != 1 {
		t.Fatalf("Acquire = %d; want 1", n)
	}
}

package worker

import (
	"context"
	"testing"
	"time"
)

func TestGate_PauseBlocksUntilResume(t *testing.T) {
	var g Gate
	if err := g.Wait(context.Background()); err != nil {
		t.Fatal("zero value must be open")
	}
	g.Pause()
	done := make(chan error, 1)
	go func() { done <- g.Wait(context.Background()) }()

	select {
	case <-done:
		t.Fatal("Wait must block while paused")
	case <-time.After(50 * time.Millisecond):
	}
	g.Resume()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	g.Resume() // idempotent
}

func TestGate_WaitHonoursContext(t *testing.T) {
	var g Gate
	g.Pause()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.Wait(ctx); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestBackoff_GrowsAndResets(t *testing.T) {
	b := Backoff{Min: 10 * time.Millisecond, Max: 80 * time.Millisecond}
	var last time.Duration
	for range 6 {
		d := b.Next()
		if d > b.Max {
			t.Fatalf("delay %s above Max", d)
		}
		last = d
	}
	if last < b.Max/2 {
		t.Fatalf("delay should approach Max, got %s", last)
	}
	b.Reset()
	if d := b.Next(); d > b.Min {
		t.Fatalf("after Reset delay=%s, want <= Min", d)
	}
}

func TestRedeliveryDelay(t *testing.T) {
	for n, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 10: 30 * time.Second} {
		got := RedeliveryDelay(time.Second, 30*time.Second, n)
		if got < want/2 || got > want {
			t.Errorf("delivery %d: %v, want within [%v, %v]", n, got, want/2, want)
		}
	}
	if got := RedeliveryDelay(time.Second, 0, 5); got > time.Second {
		t.Errorf("a cap below base must use base, got %v", got)
	}
}

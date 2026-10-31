package flight

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroup_ConcurrentCallersShareOneCall(t *testing.T) {
	var g Group
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := g.Do(context.Background(), func(context.Context) error {
				calls.Add(1)
				<-release
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("calls=%d, want 1", n)
	}
}

func TestGroup_WaiterHonorsItsOwnContext(t *testing.T) {
	var g Group
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := g.Do(ctx, func(context.Context) error { <-release; return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want the caller's deadline", err)
	}
}

func TestGroup_CallIsBoundedByTimeout(t *testing.T) {
	g := Group{Timeout: 10 * time.Millisecond}
	err := g.Do(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want the group timeout", err)
	}
}

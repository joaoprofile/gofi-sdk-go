package worker

import (
	"context"
	"sync"
	"time"
)

// minRenewEvery keeps a tiny lease from turning the renewal into a busy loop.
const minRenewEvery = 100 * time.Millisecond

// RenewEvery is the renewal period for a lease: a third of it, so two renewals
// can fail before the broker hands the message to another consumer.
func RenewEvery(lease time.Duration) time.Duration {
	return max(lease/3, minRenewEvery)
}

// KeepAlive calls renew every interval until the returned stop is called.
// Renewal ignores ctx cancellation: a shutdown drains in-flight handlers, and
// their leases must outlive it. Each renew call is bounded by interval. stop
// waits for a running renew, so no lease is renewed after the message is
// acked or deleted.
func KeepAlive(ctx context.Context, interval time.Duration, renew func(context.Context)) (stop func()) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rctx, rcancel := context.WithTimeout(ctx, interval)
				renew(rctx)
				rcancel()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// Slots counts free handler slots so a poller receives only what can start
// now; a message waiting in a local buffer would burn its lease unseen.
type Slots struct{ free chan struct{} }

// NewSlots returns n free slots (at least one).
func NewSlots(n int) *Slots {
	n = max(n, 1)
	s := &Slots{free: make(chan struct{}, n)}
	for range n {
		s.free <- struct{}{}
	}
	return s
}

// Acquire blocks until a slot is free, then takes up to limit of them
// without blocking. It returns how many it took, or ctx's error.
func (s *Slots) Acquire(ctx context.Context, limit int) (int, error) {
	select {
	case <-s.free:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	n := 1
	for n < limit {
		select {
		case <-s.free:
			n++
		default:
			return n, nil
		}
	}
	return n, nil
}

// Release returns n slots; slots beyond the capacity are dropped, so a
// broker that delivers more than asked cannot deadlock the poller.
func (s *Slots) Release(n int) {
	for range n {
		select {
		case s.free <- struct{}{}:
		default:
			return
		}
	}
}

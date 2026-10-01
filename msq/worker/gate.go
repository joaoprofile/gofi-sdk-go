package worker

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// Default receive-error backoff bounds for polling consumers.
const (
	ReceiveBackoffMin = 200 * time.Millisecond
	ReceiveBackoffMax = 30 * time.Second
)

// Gate pauses consumers without busy waiting. The zero value is open.
type Gate struct {
	mu     sync.Mutex
	resume chan struct{} // non-nil while paused
}

// Pause closes the gate; it is idempotent.
func (g *Gate) Pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.resume == nil {
		g.resume = make(chan struct{})
	}
}

// Resume opens the gate and wakes every waiter; it is idempotent.
func (g *Gate) Resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.resume != nil {
		close(g.resume)
		g.resume = nil
	}
}

// Paused reports whether the gate is closed.
func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.resume != nil
}

// Wait blocks while the gate is paused or until ctx is done.
func (g *Gate) Wait(ctx context.Context) error {
	g.mu.Lock()
	ch := g.resume
	g.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Backoff yields exponential delays with jitter between Min and Max.
type Backoff struct {
	Min, Max time.Duration
	attempt  int
}

// Next returns the delay for the current attempt and advances it.
func (b *Backoff) Next() time.Duration {
	d := b.Min << min(b.attempt, 30)
	if d <= 0 || d > b.Max {
		d = b.Max
	}
	b.attempt++
	// Equal jitter: spread retries of many consumers without dropping below d/2.
	return d/2 + rand.N(d/2+1) // #nosec G404 -- backoff jitter, not a secret
}

// Reset starts the sequence again after a success.
func (b *Backoff) Reset() { b.attempt = 0 }

// Sleep waits for d or until ctx is done.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RedeliveryDelay is the wait before delivery n+1 of a message nacked on
// delivery n (1-based): Backoff from base, capped at maxDelay.
func RedeliveryDelay(base, maxDelay time.Duration, n int) time.Duration {
	b := Backoff{Min: base, Max: max(maxDelay, base), attempt: max(n-1, 0)}
	return b.Next()
}

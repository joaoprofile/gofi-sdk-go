// Package flight runs one fetch at a time for concurrent callers (a minimal
// singleflight) for the iam providers that refresh remote documents.
package flight

import (
	"context"
	"sync"
	"time"
)

// DefaultTimeout bounds a shared fetch even with an HTTP client without timeout.
const DefaultTimeout = 30 * time.Second

// Group shares one in-flight call among concurrent callers. The call runs
// detached from the first caller's cancellation, bounded by Timeout; each
// waiter stops on its own ctx. The zero value is ready to use.
type Group struct {
	// Timeout bounds the shared call; <= 0 uses DefaultTimeout.
	Timeout time.Duration

	mu  sync.Mutex
	cur *call
}

type call struct {
	done chan struct{}
	err  error
}

// Do runs fn unless a call is already in flight, then waits for it.
func (g *Group) Do(ctx context.Context, fn func(context.Context) error) error {
	g.mu.Lock()
	c := g.cur
	if c == nil {
		c = &call{done: make(chan struct{})}
		g.cur = c
		timeout := g.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		go func() {
			defer cancel()
			c.err = fn(fctx)
			g.mu.Lock()
			g.cur = nil
			g.mu.Unlock()
			close(c.done)
		}()
	}
	g.mu.Unlock()
	select {
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

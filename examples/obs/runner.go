package main

import (
	"context"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
)

// runner adapts a background loop (queue consumer, scheduled job) to
// gofi.Runner: ListenAndServe runs it next to the HTTP server and cancels its
// context on shutdown, before telemetry is flushed.
type runner struct {
	name   string
	loop   func(context.Context)
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func newRunner(name string, loop func(context.Context)) *runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &runner{name: name, loop: loop, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (r *runner) Name() string                               { return r.name }
func (r *runner) Stage() gofi.Stage                          { return gofi.StageServer }
func (r *runner) Start(context.Context, *gofi.Runtime) error { return nil }

// Run blocks until Stop cancels the loop.
func (r *runner) Run() error {
	defer close(r.done)
	r.loop(r.ctx)
	return nil
}

// Stop cancels the loop and waits for it to return.
func (r *runner) Stop(ctx context.Context) error {
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

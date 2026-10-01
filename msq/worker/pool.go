package worker

import (
	"log/slog"
	"runtime/debug"
	"sync"
)

// Pool is a bounded goroutine pool. Jobs are dispatched through a channel;
// exactly N goroutines consume from that channel in parallel.
type Pool struct {
	jobs chan func()
	wg   sync.WaitGroup
}

// New creates a Pool of n goroutines and starts them immediately.
// Call Close to drain pending jobs and stop all goroutines.
func New(n int) *Pool {
	if n <= 0 {
		n = 1
	}
	p := &Pool{jobs: make(chan func(), n*2)}
	for i := 0; i < n; i++ {
		go p.run()
	}
	return p
}

func (p *Pool) run() {
	for job := range p.jobs {
		p.safeRun(job)
	}
}

// safeRun keeps the worker alive when a handler panics; the message is left
// unacknowledged so the broker redelivers it.
func (p *Pool) safeRun(job func()) {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("msq: job panicked", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
		}
	}()
	job()
}

// Enqueue submits a job to the pool. Blocks if all workers are busy.
func (p *Pool) Enqueue(job func()) {
	p.wg.Add(1)
	p.jobs <- job
}

// Wait blocks until all enqueued jobs complete.
func (p *Pool) Wait() { p.wg.Wait() }

// Close drains pending jobs, waits for completion, then stops all goroutines.
func (p *Pool) Close() {
	p.Wait()
	close(p.jobs)
}

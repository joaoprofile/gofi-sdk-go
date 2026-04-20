package worker

import "sync"

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
		job()
		p.wg.Done()
	}
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

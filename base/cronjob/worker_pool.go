package cronjob

import (
	"errors"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
)

// ErrPoolClosed is returned when a batch is enqueued after Close.
var ErrPoolClosed = errors.New("cronjob: worker pool is closed")

type JobGenerator[T any] struct {
	GenericObject  []T
	ProcessJobFunc func(T) error
	BatchSize      int
	// OnError receives each item whose ProcessJobFunc failed; nil logs the
	// error with slog.
	OnError func(item T, err error)
}

func NewJobGenerator[T any](genericObject []T, batchSize int, processJobFunc func(T) error) *JobGenerator[T] {
	return &JobGenerator[T]{
		GenericObject:  genericObject,
		ProcessJobFunc: processJobFunc,
		BatchSize:      batchSize,
	}
}

func (j *JobGenerator[T]) GenerateJobs() [][]func() {
	var jobBatches [][]func()
	var jobBatch []func()

	for _, item := range j.GenericObject {
		jobBatch = append(jobBatch, func() {
			if err := j.ProcessJobFunc(item); err != nil {
				j.reportError(item, err)
			}
		})

		if len(jobBatch) >= j.BatchSize {
			jobBatches = append(jobBatches, jobBatch)
			jobBatch = nil
		}
	}

	if len(jobBatch) > 0 {
		jobBatches = append(jobBatches, jobBatch)
	}

	return jobBatches
}

func (j *JobGenerator[T]) reportError(item T, err error) {
	if j.OnError != nil {
		j.OnError(item, err)
		return
	}
	slog.Error("cronjob: job failed", slog.Any("error", err))
}

// RunWithPool enqueues every batch; it stops at ErrPoolClosed.
func (j *JobGenerator[T]) RunWithPool(pool *WorkerPool) error {
	for _, batch := range j.GenerateJobs() {
		if err := pool.EnqueueJobBatch(batch); err != nil {
			return err
		}
	}
	return nil
}

type WorkerPool struct {
	Workers int
	Jobs    chan []func()
	wg      sync.WaitGroup

	mu     sync.RWMutex // guards closed against sends on a closed Jobs
	closed bool
}

// NewPool creates a pool of workers goroutines (started by Start); workers
// below 1 uses GOMAXPROCS, as a pool without workers would block forever.
func NewPool(workers int) *WorkerPool {
	if workers < 1 {
		workers = runtime.GOMAXPROCS(0)
	}
	return &WorkerPool{
		Workers: workers,
		Jobs:    make(chan []func(), workers),
	}
}

func (p *WorkerPool) Start() {
	for i := 0; i < p.Workers; i++ {
		go p.worker(i)
	}
}

func (p *WorkerPool) worker(id int) {
	for batch := range p.Jobs {
		for _, jobFunc := range batch {
			p.safeRun(jobFunc)
		}
	}
}

// EnqueueJobBatch queues batch, blocking while the queue is full. It returns
// ErrPoolClosed after Close.
func (p *WorkerPool) EnqueueJobBatch(batch []func()) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return ErrPoolClosed
	}
	p.wg.Add(len(batch))
	p.Jobs <- batch
	return nil
}

func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

// Close stops accepting batches, waits for the queued ones and stops the
// workers. Calling it again is a no-op.
func (p *WorkerPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	p.Wait()
	close(p.Jobs)
}

// safeRun keeps the worker alive and Wait unblocked when a job panics.
func (p *WorkerPool) safeRun(job func()) {
	defer p.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cronjob: job panicked", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
		}
	}()
	job()
}

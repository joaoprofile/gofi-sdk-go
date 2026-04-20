package cronjob

import (
	"log"
	"sync"
)

type JobGenerator[T any] struct {
	GenericObject  []T
	ProcessJobFunc func(T) error
	BatchSize      int
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
		item := item
		jobBatch = append(jobBatch, func() {
			err := j.ProcessJobFunc(item)
			if err != nil {
				log.Println("Error on process job:", err)
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

func (j *JobGenerator[T]) RunWithPool(pool *WorkerPool) {
	batches := j.GenerateJobs()
	for _, batch := range batches {
		pool.EnqueueJobBatch(batch)
	}
}

type WorkerPool struct {
	Workers int
	Jobs    chan []func()
	wg      sync.WaitGroup
}

func NewPool(workers int) *WorkerPool {
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
			jobFunc()
			p.wg.Done()
		}
	}
}

func (p *WorkerPool) EnqueueJobBatch(batch []func()) {
	p.wg.Add(len(batch))
	p.Jobs <- batch
}

func (p *WorkerPool) Wait() {
	p.wg.Wait()
}

func (p *WorkerPool) Close() {
	p.Wait()
	close(p.Jobs)
}

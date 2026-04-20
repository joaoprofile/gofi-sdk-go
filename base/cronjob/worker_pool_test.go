package cronjob

import (
	"errors"
	"io"
	"log"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//  WorkerPool

func TestNewPool_SetsWorkerCount(t *testing.T) {
	p := NewPool(4)
	assert.Equal(t, 4, p.Workers)
}

func TestWorkerPool_RunsAllJobs(t *testing.T) {
	const numJobs = 20
	var executed int64

	p := NewPool(4)
	p.Start()

	batch := make([]func(), numJobs)
	for i := range batch {
		batch[i] = func() { atomic.AddInt64(&executed, 1) }
	}

	p.EnqueueJobBatch(batch)
	p.Close()

	assert.Equal(t, int64(numJobs), atomic.LoadInt64(&executed))
}

func TestWorkerPool_MultipleBatches(t *testing.T) {
	var executed int64

	p := NewPool(3)
	p.Start()

	for i := 0; i < 5; i++ {
		batch := []func(){
			func() { atomic.AddInt64(&executed, 1) },
			func() { atomic.AddInt64(&executed, 1) },
		}
		p.EnqueueJobBatch(batch)
	}

	p.Close()
	assert.Equal(t, int64(10), atomic.LoadInt64(&executed))
}

func TestWorkerPool_SingleWorker(t *testing.T) {
	var executed int64

	p := NewPool(1)
	p.Start()

	batch := []func(){
		func() { atomic.AddInt64(&executed, 1) },
		func() { atomic.AddInt64(&executed, 1) },
		func() { atomic.AddInt64(&executed, 1) },
	}
	p.EnqueueJobBatch(batch)
	p.Close()

	assert.Equal(t, int64(3), atomic.LoadInt64(&executed))
}

func TestWorkerPool_EmptyBatch(t *testing.T) {
	p := NewPool(2)
	p.Start()

	assert.NotPanics(t, func() {
		p.EnqueueJobBatch([]func(){})
		p.Close()
	})
}

func TestWorkerPool_WaitBlocksUntilDone(t *testing.T) {
	var executed int64

	p := NewPool(2)
	p.Start()

	batch := make([]func(), 10)
	for i := range batch {
		batch[i] = func() { atomic.AddInt64(&executed, 1) }
	}

	p.EnqueueJobBatch(batch)
	p.Wait()

	assert.Equal(t, int64(10), atomic.LoadInt64(&executed))
	close(p.Jobs) // manual close after Wait
}

//  JobGenerator

func TestNewJobGenerator_StoresFields(t *testing.T) {
	items := []int{1, 2, 3}
	fn := func(int) error { return nil }

	g := NewJobGenerator(items, 2, fn)

	assert.Equal(t, items, g.GenericObject)
	assert.Equal(t, 2, g.BatchSize)
}

func TestJobGenerator_GenerateJobs_SingleBatch(t *testing.T) {
	items := []string{"a", "b", "c"}
	g := NewJobGenerator(items, 10, func(string) error { return nil })

	batches := g.GenerateJobs()

	require.Len(t, batches, 1)
	assert.Len(t, batches[0], 3)
}

func TestJobGenerator_GenerateJobs_MultipleBatches(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	g := NewJobGenerator(items, 2, func(int) error { return nil })

	batches := g.GenerateJobs()

	// 5 items, batch size 2 → [2, 2, 1]
	require.Len(t, batches, 3)
	assert.Len(t, batches[0], 2)
	assert.Len(t, batches[1], 2)
	assert.Len(t, batches[2], 1)
}

func TestJobGenerator_GenerateJobs_ExactBatchBoundary(t *testing.T) {
	items := []int{1, 2, 3, 4}
	g := NewJobGenerator(items, 2, func(int) error { return nil })

	batches := g.GenerateJobs()

	require.Len(t, batches, 2)
	assert.Len(t, batches[0], 2)
	assert.Len(t, batches[1], 2)
}

func TestJobGenerator_GenerateJobs_EmptyInput(t *testing.T) {
	g := NewJobGenerator([]int{}, 5, func(int) error { return nil })

	batches := g.GenerateJobs()
	assert.Empty(t, batches)
}

func TestJobGenerator_GenerateJobs_ExecutesWithCorrectItems(t *testing.T) {
	items := []int{10, 20, 30}
	var processed []int

	g := NewJobGenerator(items, 10, func(v int) error {
		processed = append(processed, v)
		return nil
	})

	for _, batch := range g.GenerateJobs() {
		for _, fn := range batch {
			fn()
		}
	}

	assert.ElementsMatch(t, items, processed)
}

func TestJobGenerator_ProcessJobFunc_ErrorIsLogged(t *testing.T) {
	// Errors from ProcessJobFunc are logged (not propagated). The job func
	// should complete without panicking even when the processor returns an error.
	prev := log.Writer()
	log.SetOutput(io.Discard) // suppress expected error log
	defer log.SetOutput(prev)

	items := []int{1, 2, 3}
	g := NewJobGenerator(items, 10, func(int) error {
		return errors.New("processing failed")
	})

	assert.NotPanics(t, func() {
		for _, batch := range g.GenerateJobs() {
			for _, fn := range batch {
				fn()
			}
		}
	})
}

func TestJobGenerator_RunWithPool_ExecutesAllItems(t *testing.T) {
	var executed int64
	items := []int{1, 2, 3, 4, 5, 6, 7, 8}

	g := NewJobGenerator(items, 3, func(int) error {
		atomic.AddInt64(&executed, 1)
		return nil
	})

	p := NewPool(4)
	p.Start()
	g.RunWithPool(p)
	p.Close()

	assert.Equal(t, int64(len(items)), atomic.LoadInt64(&executed))
}

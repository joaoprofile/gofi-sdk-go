package cronjob

import (
	"errors"
	"runtime"
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
	var executed atomic.Int64

	p := NewPool(4)
	p.Start()

	batch := make([]func(), numJobs)
	for i := range batch {
		batch[i] = func() { executed.Add(1) }
	}

	p.EnqueueJobBatch(batch)
	p.Close()

	assert.Equal(t, int64(numJobs), executed.Load())
}

func TestWorkerPool_MultipleBatches(t *testing.T) {
	var executed atomic.Int64

	p := NewPool(3)
	p.Start()

	for range 5 {
		batch := []func(){
			func() { executed.Add(1) },
			func() { executed.Add(1) },
		}
		p.EnqueueJobBatch(batch)
	}

	p.Close()
	assert.Equal(t, int64(10), executed.Load())
}

func TestWorkerPool_SingleWorker(t *testing.T) {
	var executed atomic.Int64

	p := NewPool(1)
	p.Start()

	batch := []func(){
		func() { executed.Add(1) },
		func() { executed.Add(1) },
		func() { executed.Add(1) },
	}
	p.EnqueueJobBatch(batch)
	p.Close()

	assert.Equal(t, int64(3), executed.Load())
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
	var executed atomic.Int64

	p := NewPool(2)
	p.Start()

	batch := make([]func(), 10)
	for i := range batch {
		batch[i] = func() { executed.Add(1) }
	}

	p.EnqueueJobBatch(batch)
	p.Wait()

	assert.Equal(t, int64(10), executed.Load())
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
	// Without OnError, errors are logged (not propagated) and the job func
	// completes without panicking.
	g := NewJobGenerator([]int{1, 2, 3}, 10, func(int) error {
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

func TestJobGenerator_OnErrorReceivesFailedItems(t *testing.T) {
	g := NewJobGenerator([]int{1, 2, 3}, 2, func(i int) error {
		if i%2 == 1 {
			return errors.New("odd")
		}
		return nil
	})
	var failed []int
	g.OnError = func(i int, err error) {
		assert.EqualError(t, err, "odd")
		failed = append(failed, i)
	}
	for _, batch := range g.GenerateJobs() {
		for _, fn := range batch {
			fn()
		}
	}
	assert.Equal(t, []int{1, 3}, failed)
}

func TestJobGenerator_RunWithPool_ExecutesAllItems(t *testing.T) {
	var executed atomic.Int64
	items := []int{1, 2, 3, 4, 5, 6, 7, 8}

	g := NewJobGenerator(items, 3, func(int) error {
		executed.Add(1)
		return nil
	})

	p := NewPool(4)
	p.Start()
	assert.NoError(t, g.RunWithPool(p))
	p.Close()

	assert.Equal(t, int64(len(items)), executed.Load())
}

// Regression: NewPool(0) created no workers and Enqueue blocked forever.
func TestNewPool_ZeroWorkersUsesGOMAXPROCS(t *testing.T) {
	p := NewPool(0)
	assert.Equal(t, runtime.GOMAXPROCS(0), p.Workers)
	p.Start()
	var ran atomic.Bool
	require.NoError(t, p.EnqueueJobBatch([]func(){func() { ran.Store(true) }}))
	p.Close()
	assert.True(t, ran.Load())
}

// Regression: enqueueing after Close panicked with a send on a closed channel.
func TestWorkerPool_EnqueueAfterCloseFails(t *testing.T) {
	p := NewPool(1)
	p.Start()
	p.Close()
	assert.NotPanics(t, func() {
		assert.ErrorIs(t, p.EnqueueJobBatch([]func(){func() {}}), ErrPoolClosed)
		p.Close() // idempotent
	})
	g := NewJobGenerator([]int{1}, 1, func(int) error { return nil })
	assert.ErrorIs(t, g.RunWithPool(p), ErrPoolClosed)
}

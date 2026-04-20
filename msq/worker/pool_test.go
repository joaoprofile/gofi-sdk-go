package worker_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/msq/worker"
	"github.com/stretchr/testify/assert"
)

func TestNewDefaultsToOneWhenNIsZero(t *testing.T) {
	// Pool with n=0 must not deadlock — should default to 1 worker.
	p := worker.New(0)
	defer p.Close()

	var ran atomic.Bool
	p.Enqueue(func() { ran.Store(true) })
	p.Wait()
	assert.True(t, ran.Load())
}

func TestNewDefaultsToOneWhenNIsNegative(t *testing.T) {
	p := worker.New(-5)
	defer p.Close()

	var ran atomic.Bool
	p.Enqueue(func() { ran.Store(true) })
	p.Wait()
	assert.True(t, ran.Load())
}

func TestAllJobsRun(t *testing.T) {
	const total = 50
	p := worker.New(4)
	defer p.Close()

	var count atomic.Int32
	for i := 0; i < total; i++ {
		p.Enqueue(func() { count.Add(1) })
	}
	p.Wait()

	assert.Equal(t, int32(total), count.Load())
}

func TestJobsRunConcurrently(t *testing.T) {
	// Submit 4 jobs that each take 50 ms. With 4 workers they should finish
	// in ~50 ms total, not 200 ms. We allow up to 180 ms to avoid flakiness.
	const concurrency = 4
	p := worker.New(concurrency)
	defer p.Close()

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		p.Enqueue(func() { time.Sleep(50 * time.Millisecond) })
	}
	p.Wait()
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 180*time.Millisecond,
		"jobs should run in parallel (elapsed=%s)", elapsed)
}

func TestCloseWaitsForPendingJobs(t *testing.T) {
	p := worker.New(2)

	var count atomic.Int32
	for i := 0; i < 10; i++ {
		p.Enqueue(func() { count.Add(1) })
	}

	p.Close() // must drain all 10 jobs before returning

	assert.Equal(t, int32(10), count.Load())
}

func TestWaitReturnsWhenQueueIsEmpty(t *testing.T) {
	p := worker.New(2)
	defer p.Close()

	// Wait with nothing enqueued should return immediately.
	done := make(chan struct{})
	go func() { p.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Wait() hung when queue was empty")
	}
}

func TestSingleWorker(t *testing.T) {
	p := worker.New(1)
	defer p.Close()

	results := make([]int, 0, 5)
	var mu sync.Mutex
	for i := 0; i < 5; i++ {
		n := i
		p.Enqueue(func() {
			mu.Lock()
			results = append(results, n)
			mu.Unlock()
		})
	}
	p.Wait()

	assert.Len(t, results, 5)
}

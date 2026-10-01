package cronjob

import (
	"sync/atomic"
	"testing"
)

func TestWorkerPool_SurvivesPanickingJob(t *testing.T) {
	p := NewPool(1)
	p.Start()
	var ran atomic.Bool
	p.EnqueueJobBatch([]func(){func() { panic("boom") }, func() { ran.Store(true) }})
	p.Wait()
	close(p.Jobs)

	if !ran.Load() {
		t.Fatal("the worker must keep processing after a panic")
	}
}

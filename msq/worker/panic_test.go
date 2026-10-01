package worker

import (
	"sync/atomic"
	"testing"
)

func TestPool_SurvivesPanickingJob(t *testing.T) {
	p := New(1)
	var ran atomic.Bool
	p.Enqueue(func() { panic("boom") })
	p.Enqueue(func() { ran.Store(true) })
	p.Close()

	if !ran.Load() {
		t.Fatal("the worker must keep processing after a panic")
	}
}

package cronjob

import (
	"bytes"
	"context"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

//  helpers

// counter returns a job func and a pointer to the call count.
func counter() (func(), *int64) {
	var n int64
	return func() { atomic.AddInt64(&n, 1) }, &n
}

// panicJob returns a job func that always panics.
func panicJob() func() { return func() { panic("boom") } }

//  Interval mode

func TestInterval_MultipleStopCallsSafe(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}
	h := ScheduleJob(context.Background(), cfg, func() {})

	assert.NotPanics(t, func() {
		h.Stop(context.Background())
		h.Stop(context.Background())
		h.Stop(context.Background())
	})
	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestInterval_ConcurrentGetStatus(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 5 * time.Millisecond}
	h := ScheduleJob(context.Background(), cfg, func() {})

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			h.GetStatus()
		})
	}
	wg.Wait()
	h.Stop(context.Background())
}

//  Fixed mode─

//  Invalid configuration (panics) ─

func TestScheduleJob_InvalidMode_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: "weekly"}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Interval_NonPositiveDuration_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 0}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Interval_NegativeDuration_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: -1 * time.Second}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Fixed_InvalidHour_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Fixed, Hour: 24, Minute: 0}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Fixed_NegativeHour_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Fixed, Hour: -1, Minute: 0}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Fixed_InvalidMinute_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: 60}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_Fixed_NegativeMinute_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: -1}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

func TestScheduleJob_InvalidLocation_Panics(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: time.Second, LocationName: "Not/APlace"}
	assert.Panics(t, func() {
		ScheduleJob(context.Background(), cfg, func() {})
	})
}

//  isTodayValid─

func TestIsTodayValid_EmptyWeekdays(t *testing.T) {
	for _, day := range []time.Weekday{
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday,
	} {
		t := t
		t.Run(day.String(), func(t *testing.T) {
			now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			// shift to the desired weekday
			offset := (int(day) - int(now.Weekday()) + 7) % 7
			now = now.AddDate(0, 0, offset)
			assert.True(t, isTodayValid(now, nil))
		})
	}
}

func TestIsTodayValid_MatchingWeekday(t *testing.T) {
	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // 2024-01-01 is a Monday
	assert.True(t, isTodayValid(monday, []time.Weekday{time.Monday, time.Wednesday}))
}

func TestIsTodayValid_NonMatchingWeekday(t *testing.T) {
	monday := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.False(t, isTodayValid(monday, []time.Weekday{time.Tuesday, time.Thursday}))
}

//  CheckAllJobsHealth

func TestCheckAllJobsHealth_LogsAllStatuses(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	// running job
	hRunning := &JobHandle{status: JobRunning}
	// stopped job
	hStopped := &JobHandle{status: JobStopped}
	// failed job
	hFailed := &JobHandle{status: JobFailed}

	CheckAllJobsHealth([]*JobHandle{hRunning, hStopped, hFailed})

	out := buf.String()
	assert.Contains(t, out, "running smoothly")
	assert.Contains(t, out, "has stopped")
	assert.Contains(t, out, "has failed")
}

func TestCheckAllJobsHealth_EmptySlice(t *testing.T) {
	assert.NotPanics(t, func() {
		CheckAllJobsHealth([]*JobHandle{})
	})
}

func TestStop_ZeroHandleIsNoOp(t *testing.T) {
	if err := (&JobHandle{}).Stop(context.Background()); err != nil {
		t.Fatalf("Stop on a zero handle: %v", err)
	}
}

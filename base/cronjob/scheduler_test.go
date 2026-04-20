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
	"github.com/stretchr/testify/require"
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

func TestInterval_ExecutesJob(t *testing.T) {
	job, calls := counter()
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}

	h := ScheduleJob(context.Background(), cfg, job)
	defer h.Stop()

	time.Sleep(70 * time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt64(calls), int64(2))
	assert.Equal(t, JobRunning, h.GetStatus())
}

func TestInterval_StopsJob(t *testing.T) {
	job, calls := counter()
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}

	h := ScheduleJob(context.Background(), cfg, job)
	time.Sleep(50 * time.Millisecond)
	h.Stop()

	snapshot := atomic.LoadInt64(calls)
	time.Sleep(50 * time.Millisecond) // must not increase after Stop

	assert.Equal(t, snapshot, atomic.LoadInt64(calls))
	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestInterval_ContextCancellation(t *testing.T) {
	job, _ := counter()
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	h := ScheduleJob(ctx, cfg, job)

	time.Sleep(30 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond)

	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestInterval_WeekdayFilter_ValidDay(t *testing.T) {
	now := time.Now()
	job, calls := counter()
	cfg := ScheduleConfig{
		Mode:     Interval,
		Interval: 20 * time.Millisecond,
		Weekdays: []time.Weekday{now.Weekday()}, // today is valid
	}

	h := scheduleJob(context.Background(), cfg, job, func() time.Time { return now })
	defer h.Stop()

	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt64(calls), int64(1))
}

func TestInterval_WeekdayFilter_InvalidDay(t *testing.T) {
	now := time.Now()
	// Use a weekday that is never today.
	excluded := (now.Weekday() + 1) % 7
	job, calls := counter()
	cfg := ScheduleConfig{
		Mode:     Interval,
		Interval: 20 * time.Millisecond,
		Weekdays: []time.Weekday{excluded},
	}

	h := scheduleJob(context.Background(), cfg, job, func() time.Time { return now })
	defer h.Stop()

	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, int64(0), atomic.LoadInt64(calls))
}

func TestInterval_JobPanic_SetsFailedStatus(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}

	h := ScheduleJob(context.Background(), cfg, panicJob())
	defer h.Stop()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, JobFailed, h.GetStatus())
}

func TestInterval_MultipleStopCallsSafe(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}
	h := ScheduleJob(context.Background(), cfg, func() {})

	assert.NotPanics(t, func() {
		h.Stop()
		h.Stop()
		h.Stop()
	})
	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestInterval_ConcurrentGetStatus(t *testing.T) {
	cfg := ScheduleConfig{Mode: Interval, Interval: 5 * time.Millisecond}
	h := ScheduleJob(context.Background(), cfg, func() {})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.GetStatus()
		}()
	}
	wg.Wait()
	h.Stop()
}

func TestInterval_CustomLocation(t *testing.T) {
	job, calls := counter()
	cfg := ScheduleConfig{
		Mode:         Interval,
		Interval:     20 * time.Millisecond,
		LocationName: "America/Sao_Paulo",
	}

	h := ScheduleJob(context.Background(), cfg, job)
	defer h.Stop()

	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt64(calls), int64(1))
}

//  Fixed mode─

// fixedNowFn returns a nowFn that makes the scheduler think "now" is just before
// the target hour:minute, so the timer fires after a real wait of ~lag.
// Uses time.Local to match the scheduler's default location, ensuring the wait
// is exactly `lag` regardless of the host's timezone.
func fixedNowFn(h, m int, lag time.Duration) func() time.Time {
	// Use time.Local so that now.In(loc) is a no-op and nextRun is computed
	// on the same calendar day and timezone as fakeNow.
	ref := time.Date(2000, 1, 3, h, m, 0, 0, time.Local) // Monday
	// "now" = ref - lag  →  nextRun = ref  →  wait = lag
	fakeNow := ref.Add(-lag)
	return func() time.Time { return fakeNow }
}

func TestFixed_ExecutesJob(t *testing.T) {
	const lag = 60 * time.Millisecond
	job, calls := counter()
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: 0}

	h := scheduleJob(context.Background(), cfg, job, fixedNowFn(10, 0, lag))
	defer h.Stop()

	time.Sleep(lag + 40*time.Millisecond)
	require.GreaterOrEqual(t, atomic.LoadInt64(calls), int64(1))
	assert.Equal(t, JobRunning, h.GetStatus())
}

func TestFixed_StopsBeforeFiring(t *testing.T) {
	const lag = 200 * time.Millisecond
	job, calls := counter()
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: 0}

	h := scheduleJob(context.Background(), cfg, job, fixedNowFn(10, 0, lag))

	time.Sleep(50 * time.Millisecond) // stop well before the timer fires
	h.Stop()

	time.Sleep(lag) // wait past when the timer would have fired
	assert.Equal(t, int64(0), atomic.LoadInt64(calls))
	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestFixed_WeekdayFilter_ValidDay(t *testing.T) {
	const lag = 60 * time.Millisecond
	// 2000-01-03 is a Monday.
	job, calls := counter()
	cfg := ScheduleConfig{
		Mode:     Fixed,
		Hour:     10,
		Minute:   0,
		Weekdays: []time.Weekday{time.Monday},
	}

	h := scheduleJob(context.Background(), cfg, job, fixedNowFn(10, 0, lag))
	defer h.Stop()

	time.Sleep(lag + 40*time.Millisecond)
	assert.GreaterOrEqual(t, atomic.LoadInt64(calls), int64(1))
}

func TestFixed_WeekdayFilter_InvalidDay(t *testing.T) {
	const lag = 60 * time.Millisecond
	// 2000-01-03 is a Monday; exclude Monday so the job should NOT run.
	job, calls := counter()
	cfg := ScheduleConfig{
		Mode:     Fixed,
		Hour:     10,
		Minute:   0,
		Weekdays: []time.Weekday{time.Tuesday},
	}

	h := scheduleJob(context.Background(), cfg, job, fixedNowFn(10, 0, lag))
	defer h.Stop()

	time.Sleep(lag + 40*time.Millisecond)
	assert.Equal(t, int64(0), atomic.LoadInt64(calls))
}

func TestFixed_ContextCancellation(t *testing.T) {
	const lag = 200 * time.Millisecond
	job, calls := counter()
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: 0}

	ctx, cancel := context.WithCancel(context.Background())
	h := scheduleJob(ctx, cfg, job, fixedNowFn(10, 0, lag))

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(lag)

	assert.Equal(t, int64(0), atomic.LoadInt64(calls))
	assert.Equal(t, JobStopped, h.GetStatus())
}

func TestFixed_JobPanic_SetsFailedStatus(t *testing.T) {
	const lag = 60 * time.Millisecond
	cfg := ScheduleConfig{Mode: Fixed, Hour: 10, Minute: 0}

	h := scheduleJob(context.Background(), cfg, panicJob(), fixedNowFn(10, 0, lag))
	defer h.Stop()

	time.Sleep(lag + 40*time.Millisecond)
	assert.Equal(t, JobFailed, h.GetStatus())
}

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

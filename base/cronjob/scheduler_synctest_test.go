package cronjob

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// These tests run in a synctest bubble: time is virtual, starts at
// 2000-01-01 00:00 UTC (a Saturday) and advances only when every goroutine
// is blocked, so counts are exact and hours pass instantly.

// after sleeps d of virtual time and waits for the scheduler to settle.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestInterval_ExecutesOnEveryTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}, job)
		defer h.Stop(context.Background())

		after(70 * time.Millisecond) // ticks at 20, 40, 60
		assert.Equal(t, int64(3), atomic.LoadInt64(calls))
		assert.Equal(t, JobRunning, h.GetStatus())
	})
}

func TestInterval_StopsJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}, job)
		after(50 * time.Millisecond)
		h.Stop(context.Background())
		after(time.Second)

		assert.Equal(t, int64(2), atomic.LoadInt64(calls), "no run after Stop")
		assert.Equal(t, JobStopped, h.GetStatus())
	})
}

func TestInterval_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		h := ScheduleJob(ctx, ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}, func() {})
		after(30 * time.Millisecond)
		cancel()
		synctest.Wait()
		assert.Equal(t, JobStopped, h.GetStatus())
	})
}

func TestInterval_WeekdayFilter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		today := time.Now().Weekday()
		valid, validCalls := counter()
		invalid, invalidCalls := counter()
		cfg := ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}
		cfg.Weekdays = []time.Weekday{today}
		h1 := ScheduleJob(context.Background(), cfg, valid)
		cfg.Weekdays = []time.Weekday{(today + 1) % 7}
		h2 := ScheduleJob(context.Background(), cfg, invalid)
		defer h1.Stop(context.Background())
		defer h2.Stop(context.Background())

		after(50 * time.Millisecond)
		assert.Equal(t, int64(2), atomic.LoadInt64(validCalls))
		assert.Zero(t, atomic.LoadInt64(invalidCalls))
	})
}

func TestInterval_JobPanic_SetsFailedStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond}, panicJob())
		defer h.Stop(context.Background())
		after(20 * time.Millisecond)
		assert.Equal(t, JobFailed, h.GetStatus())
	})
}

func TestInterval_CustomLocation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: 20 * time.Millisecond, LocationName: "America/Sao_Paulo"}, job)
		defer h.Stop(context.Background())
		after(50 * time.Millisecond)
		assert.Equal(t, int64(2), atomic.LoadInt64(calls))
	})
}

func fixedAt(hour int, weekdays ...time.Weekday) ScheduleConfig {
	return ScheduleConfig{Mode: Fixed, Hour: hour, Minute: 0, LocationName: "UTC", Weekdays: weekdays}
}

func TestFixed_RunsDailyAtTheHour(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), fixedAt(3), job)
		defer h.Stop(context.Background())

		after(3*time.Hour - time.Second)
		assert.Zero(t, atomic.LoadInt64(calls), "not before 03:00")
		after(time.Second)
		assert.Equal(t, int64(1), atomic.LoadInt64(calls), "at 03:00")
		after(24 * time.Hour)
		assert.Equal(t, int64(2), atomic.LoadInt64(calls), "again the next day")
		assert.Equal(t, JobRunning, h.GetStatus())
	})
}

func TestFixed_StopsBeforeFiring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), fixedAt(3), job)
		after(time.Hour)
		h.Stop(context.Background())
		after(48 * time.Hour)
		assert.Zero(t, atomic.LoadInt64(calls))
		assert.Equal(t, JobStopped, h.GetStatus())
	})
}

func TestFixed_WeekdayFilter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), fixedAt(3, time.Sunday), job) // 2000-01-01 is a Saturday
		defer h.Stop(context.Background())

		after(4 * time.Hour)
		assert.Zero(t, atomic.LoadInt64(calls), "Saturday is filtered out")
		after(24 * time.Hour)
		assert.Equal(t, int64(1), atomic.LoadInt64(calls), "runs on Sunday")
	})
}

func TestFixed_ContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		ctx, cancel := context.WithCancel(context.Background())
		h := ScheduleJob(ctx, fixedAt(3), job)
		after(time.Hour)
		cancel()
		after(48 * time.Hour)
		assert.Zero(t, atomic.LoadInt64(calls))
		assert.Equal(t, JobStopped, h.GetStatus())
	})
}

func TestFixed_JobPanic_SetsFailedStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := ScheduleJob(context.Background(), fixedAt(3), panicJob())
		defer h.Stop(context.Background())
		after(3 * time.Hour)
		assert.Equal(t, JobFailed, h.GetStatus())
	})
}

// Regression: Stop returned while the job was still running.
func TestStop_WaitsForRunningJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var finished atomic.Bool
		h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute}, func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(5 * time.Second) // cleanup after cancellation
			finished.Store(true)
			return nil
		})
		after(time.Minute + time.Second)
		assert.NoError(t, h.Stop(context.Background()))
		assert.True(t, finished.Load(), "Stop must wait for the run in progress")
		assert.Equal(t, JobStopped, h.GetStatus())
	})
}

func TestStop_ReturnsWhenContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute}, func(context.Context) error {
			<-release // ignores cancellation
			return nil
		})
		after(time.Minute + time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		assert.ErrorIs(t, h.Stop(ctx), context.DeadlineExceeded)
		assert.Equal(t, JobStopped, h.GetStatus())
		close(release)
		assert.NoError(t, h.Stop(context.Background()))
	})
}

// Regression: JobFailed was never reset after a later successful run.
func TestStatus_FailedResetsOnSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute}, func(context.Context) error {
			switch calls.Add(1) {
			case 1:
				return errors.New("first run fails")
			case 2:
				panic("second run panics")
			}
			return nil
		})
		defer h.Stop(context.Background())
		after(time.Minute)
		assert.Equal(t, JobFailed, h.GetStatus())
		after(time.Minute)
		assert.Equal(t, JobFailed, h.GetStatus())
		after(time.Minute)
		assert.Equal(t, JobRunning, h.GetStatus())
	})
}

package cronjob

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type ScheduleMode string

const (
	Interval ScheduleMode = "interval"
	Fixed    ScheduleMode = "fixed"
)

type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobStopped JobStatus = "stopped"
	JobFailed  JobStatus = "failed"
)

type ScheduleConfig struct {
	Mode         ScheduleMode   // Scheduling mode
	Interval     time.Duration  // Used in 'interval' mode
	Hour         int            // Used in 'fixed' mode
	Minute       int            // Used in 'fixed' mode
	Weekdays     []time.Weekday // If empty, runs every day
	LocationName string         // Optional, if empty, uses the system's timezone
}

type JobHandle struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	status JobStatus
}

func (j *JobHandle) setStatus(s JobStatus) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = s
}

func (j *JobHandle) Stop() {
	if j.cancel != nil {
		j.cancel()
		j.setStatus(JobStopped)
	}
}

func (j *JobHandle) GetStatus() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// ScheduleJob starts a background job according to cfg and returns a handle to control it.
// Panics if cfg is invalid (invalid mode, non-positive interval, out-of-range hour/minute, or unknown location).
func ScheduleJob(ctx context.Context, cfg ScheduleConfig, job func()) *JobHandle {
	return scheduleJob(ctx, cfg, job, time.Now)
}

// scheduleJob is the internal, testable implementation. nowFn replaces time.Now so tests can
// control the perceived current time without actually sleeping.
func scheduleJob(ctx context.Context, cfg ScheduleConfig, job func(), nowFn func() time.Time) *JobHandle {
	loc := time.Local
	if cfg.LocationName != "" {
		var err error
		loc, err = time.LoadLocation(cfg.LocationName)
		if err != nil {
			panic(fmt.Sprintf("error on LoadLocation: %v", err))
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	handle := &JobHandle{cancel: cancel, status: JobRunning}

	switch cfg.Mode {
	case Interval:
		if cfg.Interval <= 0 {
			cancel()
			panic("interval mode requires a positive Interval duration")
		}

		go func() {
			ticker := time.NewTicker(cfg.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					handle.setStatus(JobStopped)
					return
				case <-ticker.C:
					now := nowFn().In(loc)
					if isTodayValid(now, cfg.Weekdays) {
						runJob(job, handle)
					}
				}
			}
		}()

	case Fixed:
		if cfg.Hour < 0 || cfg.Hour > 23 {
			cancel()
			panic("fixed mode requires Hour to be between 0 and 23")
		}
		if cfg.Minute < 0 || cfg.Minute > 59 {
			cancel()
			panic("fixed mode requires Minute to be between 0 and 59")
		}

		go func() {
			for {
				now := nowFn().In(loc)
				nextRun := time.Date(now.Year(), now.Month(), now.Day(), cfg.Hour, cfg.Minute, 0, 0, loc)

				if now.After(nextRun) {
					nextRun = nextRun.Add(24 * time.Hour)
				}

				timer := time.NewTimer(nextRun.Sub(now))

				select {
				case <-ctx.Done():
					timer.Stop()
					handle.setStatus(JobStopped)
					return
				case <-timer.C:
					if isTodayValid(nextRun, cfg.Weekdays) {
						runJob(job, handle)
					}
				}
			}
		}()

	default:
		cancel()
		panic("invalid mode in ScheduleJob. Valid modes: 'interval' or 'fixed'")
	}

	return handle
}

// runJob executes the job and recovers from panics, marking the handle as failed on panic.
func runJob(job func(), handle *JobHandle) {
	defer func() {
		if r := recover(); r != nil {
			handle.setStatus(JobFailed)
		}
	}()
	job()
}

func isTodayValid(t time.Time, weekdays []time.Weekday) bool {
	if len(weekdays) == 0 {
		return true
	}
	for _, d := range weekdays {
		if t.Weekday() == d {
			return true
		}
	}
	return false
}

func CheckAllJobsHealth(jobHandles []*JobHandle) {
	for _, jobHandle := range jobHandles {
		switch jobHandle.GetStatus() {
		case JobFailed:
			log.Println("Job has failed. Check the logs.")
		case JobRunning:
			log.Println("Job is running smoothly.")
		case JobStopped:
			log.Println("Job has stopped.")
		}
	}
}

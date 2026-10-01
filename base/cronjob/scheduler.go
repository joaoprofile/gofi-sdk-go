package cronjob

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"runtime/debug"
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

	// Name identifies the job across replicas; required with Locker.
	Name string
	// Locker makes each scheduled run execute on one replica only and keeps
	// runs from overlapping across replicas (see RedisLocker). Nil runs the
	// job on every replica.
	Locker Locker
	// LockTTL is the lease ttl with a Locker, renewed every third of it while
	// the job runs. Zero uses DefaultLockTTL.
	LockTTL time.Duration
}

type JobHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	status JobStatus
}

// setStatus records s; once stopped, the handle stays stopped.
func (j *JobHandle) setStatus(s JobStatus) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != JobStopped {
		j.status = s
	}
}

// Stop cancels the schedule and the context of the run in progress, then
// waits for that run to return. It returns ctx.Err() if ctx ends first; the
// run keeps going in the background. Calling it again is safe.
func (j *JobHandle) Stop(ctx context.Context) error {
	if j.cancel == nil {
		return nil
	}
	j.cancel()
	j.setStatus(JobStopped)
	select {
	case <-j.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetStatus returns JobRunning while scheduled and the last run succeeded,
// JobFailed when the last run returned an error or panicked, and JobStopped
// after Stop or the parent context ends.
func (j *JobHandle) GetStatus() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// ScheduleJob is ScheduleJobCtx for a job that takes no context and cannot fail.
func ScheduleJob(ctx context.Context, cfg ScheduleConfig, job func()) *JobHandle {
	return ScheduleJobCtx(ctx, cfg, func(context.Context) error {
		job()
		return nil
	})
}

// ScheduleJobCtx starts a background job according to cfg and returns a
// handle to control it. Each run receives a context cancelled by Stop, by the
// parent ctx or when its lease is lost; an error or panic marks the handle
// JobFailed until a later run succeeds.
// Panics if cfg is invalid (invalid mode, non-positive interval, out-of-range
// hour/minute, unknown location, or a Locker without Name).
func ScheduleJobCtx(ctx context.Context, cfg ScheduleConfig, job func(context.Context) error) *JobHandle {
	return scheduleJob(ctx, cfg, job, time.Now)
}

// scheduler runs one scheduled job.
type scheduler struct {
	cfg     ScheduleConfig
	loc     *time.Location
	lockTTL time.Duration
	job     func(context.Context) error
	handle  *JobHandle
	now     func() time.Time
}

// scheduleJob is the internal, testable implementation. nowFn replaces time.Now so tests can
// control the perceived current time without actually sleeping.
func scheduleJob(ctx context.Context, cfg ScheduleConfig, job func(context.Context) error, nowFn func() time.Time) *JobHandle {
	s := newScheduler(cfg, job, nowFn)
	ctx, cancel := context.WithCancel(ctx)
	s.handle = &JobHandle{cancel: cancel, done: make(chan struct{}), status: JobRunning}
	go func() {
		defer close(s.handle.done)
		defer s.handle.setStatus(JobStopped)
		if cfg.Mode == Interval {
			s.runInterval(ctx)
		} else {
			s.runFixed(ctx)
		}
	}()
	return s.handle
}

// newScheduler validates cfg, panicking on a programmer error.
func newScheduler(cfg ScheduleConfig, job func(context.Context) error, nowFn func() time.Time) *scheduler {
	loc := time.Local
	if cfg.LocationName != "" {
		var err error
		loc, err = time.LoadLocation(cfg.LocationName)
		if err != nil {
			panic(fmt.Sprintf("error on LoadLocation: %v", err))
		}
	}
	if cfg.Locker != nil && cfg.Name == "" {
		panic("cronjob: ScheduleConfig.Name is required with a Locker")
	}
	validateMode(cfg)
	lockTTL := cfg.LockTTL
	if lockTTL <= 0 {
		lockTTL = DefaultLockTTL
	}
	return &scheduler{cfg: cfg, loc: loc, lockTTL: lockTTL, job: job, now: nowFn}
}

func validateMode(cfg ScheduleConfig) {
	switch cfg.Mode {
	case Interval:
		if cfg.Interval <= 0 {
			panic("interval mode requires a positive Interval duration")
		}
	case Fixed:
		if cfg.Hour < 0 || cfg.Hour > 23 {
			panic("fixed mode requires Hour to be between 0 and 23")
		}
		if cfg.Minute < 0 || cfg.Minute > 59 {
			panic("fixed mode requires Minute to be between 0 and 59")
		}
	default:
		panic("invalid mode in ScheduleJob. Valid modes: 'interval' or 'fixed'")
	}
}

func (s *scheduler) runInterval(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := s.now().In(s.loc)
			if isTodayValid(now, s.cfg.Weekdays) {
				s.fire(ctx, now.Truncate(s.cfg.Interval), s.cfg.Interval)
			}
		}
	}
}

func (s *scheduler) runFixed(ctx context.Context) {
	var lastRun time.Time
	for {
		now := s.now().In(s.loc)
		// Timers follow the monotonic clock; if the wall clock lags
		// (NTP step) never schedule at or before the run just done.
		if now.Before(lastRun) {
			now = lastRun
		}
		nextRun := nextFixedRun(now, s.cfg.Hour, s.cfg.Minute, s.loc)
		timer := time.NewTimer(nextRun.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			lastRun = nextRun
			if isTodayValid(nextRun, s.cfg.Weekdays) {
				s.fire(ctx, nextRun, fixedSlotTTL)
			}
		}
	}
}

// fire executes the run scheduled for slot. With a Locker the run holds the
// job's lease, renewed while it lasts, so a run longer than the interval never
// overlaps another replica's; the slot claim keeps each slot to one run.
func (s *scheduler) fire(ctx context.Context, slot time.Time, slotTTL time.Duration) {
	if s.cfg.Locker == nil {
		s.run(ctx)
		return
	}
	token, ok := s.acquire(ctx, leaseKey(s.cfg.Name), s.lockTTL)
	if !ok {
		return
	}
	defer s.release(ctx, token)
	if _, ok := s.acquire(ctx, slotKey(s.cfg.Name, slot), slotTTL); !ok {
		return
	}

	runCtx, cancel := context.WithCancel(context.WithValue(ctx, fencingKey{}, token))
	defer cancel()
	renewing := s.keepAlive(runCtx, cancel, token)
	s.run(runCtx)
	cancel()
	<-renewing
}

// run executes the job, recovering panics; the outcome sets the handle status.
func (s *scheduler) run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("cronjob: scheduled job panicked", slog.String("job", s.cfg.Name), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			s.handle.setStatus(JobFailed)
		}
	}()
	if err := s.job(ctx); err != nil {
		slog.Error("cronjob: scheduled job failed", slog.String("job", s.cfg.Name), slog.Any("error", err))
		s.handle.setStatus(JobFailed)
		return
	}
	s.handle.setStatus(JobRunning)
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

// nextFixedRun returns the next wall-clock hour:minute in loc. The next day is built
// with time.Date, not +24h, so runs keep their local time across DST changes.
// nextFixedRun returns the first hour:minute strictly after now: at exactly
// the scheduled instant it is already tomorrow's run.
func nextFixedRun(now time.Time, hour, minute int, loc *time.Location) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, hour, minute, 0, 0, loc)
	}
	return next
}

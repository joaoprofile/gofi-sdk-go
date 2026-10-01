package cronjob

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// memLocker is an in-memory Locker shared by simulated replicas. Expiry
// follows time.Now, which is virtual inside a synctest bubble.
type memLocker struct {
	mu       sync.Mutex
	held     map[string]memHold
	fence    int64
	err      error // returned by Acquire
	renewErr error // returned by Renew
	lose     bool  // Renew reports the lease lost
}

type memHold struct {
	token   int64
	expires time.Time
}

func newMemLocker() *memLocker { return &memLocker{held: map[string]memHold{}} }

func (m *memLocker) Acquire(_ context.Context, key string, ttl time.Duration) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return 0, false, m.err
	}
	if h, ok := m.held[key]; ok && time.Now().Before(h.expires) {
		return 0, false, nil
	}
	m.fence++
	m.held[key] = memHold{token: m.fence, expires: time.Now().Add(ttl)}
	return m.fence, true, nil
}

func (m *memLocker) Renew(_ context.Context, key string, token int64, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renewErr != nil {
		return false, m.renewErr
	}
	h, ok := m.held[key]
	if m.lose || !ok || h.token != token || !time.Now().Before(h.expires) {
		return false, nil
	}
	m.held[key] = memHold{token: token, expires: time.Now().Add(ttl)}
	return true, nil
}

func (m *memLocker) Release(_ context.Context, key string, token int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.held[key]; ok && h.token == token {
		delete(m.held, key)
	}
	return nil
}

// Three replicas scheduling the same job run each slot once.
func TestLocker_OneReplicaPerSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := newMemLocker()
		job, calls := counter()
		for range 3 {
			h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Fixed, Hour: 3, LocationName: "UTC", Name: "report", Locker: lock}, job)
			defer h.Stop(context.Background())
		}
		after(3 * time.Hour)
		after(24 * time.Hour)
		if got := atomic.LoadInt64(calls); got != 2 {
			t.Fatalf("calls=%d, want 2 (one per day)", got)
		}
	})
}

func TestLocker_IntervalReplicas(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := newMemLocker()
		job, calls := counter()
		for range 3 {
			h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute, Name: "sync", Locker: lock}, job)
			defer h.Stop(context.Background())
		}
		after(3 * time.Minute)
		if got := atomic.LoadInt64(calls); got != 3 {
			t.Fatalf("calls=%d, want 3", got)
		}
	})
}

// Regression: the slot claim expired with the interval, so a run longer than
// the interval overlapped the next slot's run on another replica.
func TestLocker_LongRunNeverOverlaps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := newMemLocker()
		var running, maxRunning, runs atomic.Int32
		job := func(ctx context.Context) error {
			n := running.Add(1)
			defer running.Add(-1)
			if n > maxRunning.Load() {
				maxRunning.Store(n)
			}
			runs.Add(1)
			time.Sleep(150 * time.Second) // 2.5 intervals, beyond the lease ttl
			return nil
		}
		for range 3 {
			h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute, Name: "slow", Locker: lock, LockTTL: 30 * time.Second}, job)
			defer h.Stop(context.Background())
		}
		after(10 * time.Minute)
		if got := maxRunning.Load(); got != 1 {
			t.Fatalf("max concurrent runs=%d, want 1", got)
		}
		if got := runs.Load(); got < 2 {
			t.Fatalf("runs=%d, want the job to keep running", got)
		}
	})
}

func TestLocker_FencingTokenGrows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var tokens []int64
		var mu sync.Mutex
		h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute, Name: "f", Locker: newMemLocker()},
			func(ctx context.Context) error {
				tok, ok := FencingToken(ctx)
				if !ok {
					t.Error("no fencing token in the run context")
				}
				mu.Lock()
				tokens = append(tokens, tok)
				mu.Unlock()
				return nil
			})
		defer h.Stop(context.Background())
		after(3 * time.Minute)
		mu.Lock()
		defer mu.Unlock()
		if len(tokens) != 3 {
			t.Fatalf("runs=%d, want 3", len(tokens))
		}
		for i := 1; i < len(tokens); i++ {
			if tokens[i] <= tokens[i-1] {
				t.Fatalf("tokens %v must grow", tokens)
			}
		}
	})
}

func TestFencingToken_AbsentWithoutLocker(t *testing.T) {
	if _, ok := FencingToken(context.Background()); ok {
		t.Error("FencingToken must report false without a lease")
	}
}

// A lost lease cancels the run so it stops before another replica takes over.
func TestLocker_LostLeaseCancelsRun(t *testing.T) {
	for name, lock := range map[string]*memLocker{
		"lost":        {held: map[string]memHold{}, lose: true},
		"unrenewable": {held: map[string]memHold{}, renewErr: errors.New("down")},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var cancelledAfter atomic.Int64
				h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Hour, Name: "l", Locker: lock, LockTTL: 30 * time.Second},
					func(ctx context.Context) error {
						start := time.Now()
						<-ctx.Done()
						cancelledAfter.Store(int64(time.Since(start)))
						return ctx.Err()
					})
				defer h.Stop(context.Background())
				after(time.Hour + time.Minute)
				got := time.Duration(cancelledAfter.Load())
				if got == 0 || got >= 30*time.Second {
					t.Fatalf("run cancelled after %s, want within the 30s lease", got)
				}
				if s := h.GetStatus(); s != JobFailed {
					t.Errorf("status=%s, want failed", s)
				}
			})
		})
	}
}

// A transient renew error keeps the run while the lease is still valid.
func TestLocker_TransientRenewErrorKeepsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := newMemLocker()
		var finished atomic.Bool
		h := ScheduleJobCtx(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Hour, Name: "t", Locker: lock, LockTTL: 30 * time.Second},
			func(ctx context.Context) error {
				lock.mu.Lock()
				lock.renewErr = errors.New("blip")
				lock.mu.Unlock()
				time.Sleep(11 * time.Second) // one failed renewal at 10s
				lock.mu.Lock()
				lock.renewErr = nil
				lock.mu.Unlock()
				time.Sleep(30 * time.Second)
				finished.Store(ctx.Err() == nil)
				return nil
			})
		defer h.Stop(context.Background())
		after(time.Hour + time.Minute)
		if !finished.Load() {
			t.Fatal("a single failed renewal must not cancel the run")
		}
	})
}

func TestLocker_ErrorSkipsRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		h := ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute, Name: "x", Locker: &memLocker{err: errors.New("down")}}, job)
		defer h.Stop(context.Background())
		after(2 * time.Minute)
		if got := atomic.LoadInt64(calls); got != 0 {
			t.Fatalf("calls=%d, want 0", got)
		}
	})
}

func TestLocker_RequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a Locker without Name must panic")
		}
	}()
	ScheduleJob(context.Background(), ScheduleConfig{Mode: Interval, Interval: time.Minute, Locker: newMemLocker()}, func() {})
}

func newRedisLocker(t *testing.T) (RedisLocker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return RedisLocker{Client: goredis.NewClient(&goredis.Options{Addr: mr.Addr()})}, mr
}

func TestRedisLocker_AcquireAndRenew(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()
	key := leaseKey("job")

	first, ok, err := l.Acquire(ctx, key, time.Minute)
	if err != nil || !ok || first <= 0 {
		t.Fatalf("first=%d ok=%v err=%v", first, ok, err)
	}
	if _, second, _ := l.Acquire(ctx, key, time.Minute); second {
		t.Error("second acquire must lose")
	}
	if renewed, err := l.Renew(ctx, key, first+1, time.Minute); err != nil || renewed {
		t.Errorf("renew with a foreign token=%v,%v; want false", renewed, err)
	}
	if renewed, err := l.Renew(ctx, key, first, 2*time.Minute); err != nil || !renewed {
		t.Errorf("renew=%v,%v; want true", renewed, err)
	}
	mr.FastForward(90 * time.Second)
	if _, again, _ := l.Acquire(ctx, key, time.Minute); again {
		t.Error("a renewed lease must still be held")
	}
	mr.FastForward(time.Minute)
	if _, again, _ := l.Acquire(ctx, key, time.Minute); !again {
		t.Error("an expired lease must be acquirable")
	}
}

func TestRedisLocker_ReleaseAndFencing(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()
	key := leaseKey("job")

	first, _, err := l.Acquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(ctx, key, first+1); err != nil || !mr.Exists(key) {
		t.Errorf("release with a foreign token must keep the lease: %v", err)
	}
	if err := l.Release(ctx, key, first); err != nil || mr.Exists(key) {
		t.Errorf("release must free the lease: %v", err)
	}
	next, ok, err := l.Acquire(ctx, key, time.Minute)
	if err != nil || !ok || next <= first {
		t.Fatalf("next=%d ok=%v err=%v; want a larger token than %d", next, ok, err, first)
	}
	if fenceKey(key) != "gofi:cron:{job}:fence" || !mr.Exists(fenceKey(key)) {
		t.Errorf("fence key %q", fenceKey(key))
	}
}

func TestFenceKey_WithoutHashTag(t *testing.T) {
	if got := fenceKey("plain"); got != "plain:fence" {
		t.Errorf("fenceKey=%q", got)
	}
}

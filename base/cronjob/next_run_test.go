package cronjob

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestNextFixedRun_DST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata not available:", err)
	}
	// 2026-03-08 02:00 is the US spring-forward transition.
	now := time.Date(2026, 3, 7, 10, 0, 0, 0, ny)
	got := nextFixedRun(now, 9, 0, ny)
	want := time.Date(2026, 3, 8, 9, 0, 0, 0, ny)
	if !got.Equal(want) {
		t.Fatalf("next run=%s, want %s", got, want)
	}
}

func TestNextFixedRun_LaterToday(t *testing.T) {
	now := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	if got := nextFixedRun(now, 9, 30, time.UTC); !got.Equal(time.Date(2026, 1, 10, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("got %s", got)
	}
}

// At the scheduled instant the next run is tomorrow, never "now" again.
func TestNextFixedRun_AtTheInstantIsTomorrow(t *testing.T) {
	now := time.Date(2026, 1, 10, 9, 30, 0, 0, time.UTC)
	if got := nextFixedRun(now, 9, 30, time.UTC); !got.Equal(now.AddDate(0, 0, 1)) {
		t.Fatalf("got %s", got)
	}
}

// A wall clock stepped back (NTP) while waiting must not repeat the run.
func TestFixed_ClockStepBackDoesNotRepeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		job, calls := counter()
		var reads atomic.Int32
		stepped := func() time.Time {
			if reads.Add(1) == 1 {
				return time.Now() // accurate when scheduling
			}
			return time.Now().Add(-time.Second) // stepped back afterwards
		}
		h := scheduleJob(context.Background(), fixedAt(3), func(context.Context) error { job(); return nil }, stepped)
		defer h.Stop(context.Background())
		after(3*time.Hour + time.Second)
		after(time.Minute)
		if got := atomic.LoadInt64(calls); got != 1 {
			t.Fatalf("calls=%d, want 1", got)
		}
	})
}

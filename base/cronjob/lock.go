package cronjob

import (
	"context"
	"log/slog"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Locker coordinates replicas running the same job: a key is held by one
// holder at a time until its ttl expires or it is released.
type Locker interface {
	// Acquire takes key for ttl. ok is false when another holder has it. The
	// token identifies this holder and grows with every successful Acquire of
	// the job's keys, so it doubles as a fencing token.
	Acquire(ctx context.Context, key string, ttl time.Duration) (token int64, ok bool, err error)
	// Renew extends key to ttl while token still holds it; false means the
	// key was lost (expired or taken by another holder).
	Renew(ctx context.Context, key string, token int64, ttl time.Duration) (bool, error)
	// Release frees key if token still holds it.
	Release(ctx context.Context, key string, token int64) error
}

const (
	// DefaultLockTTL is the lease ttl when ScheduleConfig.LockTTL is zero.
	DefaultLockTTL = 30 * time.Second

	// fixedSlotTTL keeps a fixed-time claim long enough to cover clock skew
	// between replicas, and far shorter than the next daily run.
	fixedSlotTTL = time.Hour

	releaseTimeout = 5 * time.Second
)

type fencingKey struct{}

// FencingToken returns the token of the lease held by the running job. Pass
// it to the systems the job writes to so they can reject a holder whose lease
// already expired (e.g. UPDATE ... WHERE fence < $token). ok is false when
// the job has no Locker.
func FencingToken(ctx context.Context) (int64, bool) {
	t, ok := ctx.Value(fencingKey{}).(int64)
	return t, ok
}

// The {name} hash tag keeps a job's keys in one Redis Cluster slot.
func leaseKey(name string) string { return "gofi:cron:{" + name + "}:lease" }

func slotKey(name string, slot time.Time) string {
	return "gofi:cron:{" + name + "}:slot:" + slot.UTC().Format(time.RFC3339Nano)
}

// acquire takes key, logging lock errors: on error the run is skipped, as a
// missed run is safer than a duplicated one.
func (s *scheduler) acquire(ctx context.Context, key string, ttl time.Duration) (int64, bool) {
	token, ok, err := s.cfg.Locker.Acquire(ctx, key, ttl)
	if err != nil {
		slog.Error("cronjob: lock failed, skipping run", slog.String("job", s.cfg.Name), slog.Any("error", err))
		return 0, false
	}
	return token, ok
}

// release frees the lease even when ctx was cancelled by Stop.
func (s *scheduler) release(ctx context.Context, token int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	if err := s.cfg.Locker.Release(ctx, leaseKey(s.cfg.Name), token); err != nil {
		slog.Warn("cronjob: lease release failed; it expires on its own", slog.String("job", s.cfg.Name), slog.Any("error", err))
	}
}

// keepAlive renews the lease every third of its ttl until ctx ends. When the
// lease is lost, or cannot be renewed for two thirds of its ttl, it calls
// lost so the job stops before another replica can take over.
func (s *scheduler) keepAlive(ctx context.Context, lost context.CancelFunc, token int64) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.lockTTL / 3)
		defer ticker.Stop()
		renewed := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			ok, err := s.cfg.Locker.Renew(ctx, leaseKey(s.cfg.Name), token, s.lockTTL)
			switch {
			case ctx.Err() != nil:
				return
			case err == nil && ok:
				renewed = time.Now()
				continue
			case err != nil && time.Since(renewed) < s.lockTTL*2/3:
				continue // transient: retry on the next tick
			}
			slog.Error("cronjob: lease lost, cancelling run", slog.String("job", s.cfg.Name), slog.Any("error", err))
			lost()
			return
		}
	}()
	return done
}

// RedisLocker implements Locker on Redis. Tokens come from a per-job counter
// (gofi:cron:{name}:fence) that is never reset, so they keep growing across
// leases and replicas.
type RedisLocker struct {
	Client goredis.UniversalClient
}

var (
	acquireScript = goredis.NewScript(`
if redis.call('exists', KEYS[1]) == 1 then return 0 end
local token = redis.call('incr', KEYS[2])
redis.call('set', KEYS[1], token, 'px', ARGV[1])
return token`)
	renewScript = goredis.NewScript(`
if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('pexpire', KEYS[1], ARGV[2]) end
return 0`)
	releaseScript = goredis.NewScript(`
if redis.call('get', KEYS[1]) == ARGV[1] then return redis.call('del', KEYS[1]) end
return 0`)
)

// Acquire implements Locker.
func (l RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (int64, bool, error) {
	token, err := acquireScript.Run(ctx, l.Client, []string{key, fenceKey(key)}, ttl.Milliseconds()).Int64()
	if err != nil {
		return 0, false, err
	}
	return token, token > 0, nil
}

// Renew implements Locker.
func (l RedisLocker) Renew(ctx context.Context, key string, token int64, ttl time.Duration) (bool, error) {
	n, err := renewScript.Run(ctx, l.Client, []string{key}, token, ttl.Milliseconds()).Int64()
	return n == 1, err
}

// Release implements Locker.
func (l RedisLocker) Release(ctx context.Context, key string, token int64) error {
	return releaseScript.Run(ctx, l.Client, []string{key}, token).Err()
}

// fenceKey is the job's token counter: key up to its {hash tag} + ":fence".
func fenceKey(key string) string {
	if i := strings.IndexByte(key, '}'); i >= 0 {
		return key[:i+1] + ":fence"
	}
	return key + ":fence"
}

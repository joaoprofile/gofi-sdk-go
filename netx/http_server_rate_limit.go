package netx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"

	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

// RateLimiterBackend abstracts the rate-limiting backend, decoupling the
// middleware from a concrete Redis client and making it fully testable without
// a live Redis instance.
type RateLimiterBackend interface {
	Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error)
}

// NewRedisBackend wraps a redis.UniversalClient into a RateLimiterBackend using
// a sliding-window algorithm. Call this at the composition root where the Redis
// client is already wired.
func NewRedisBackend(client redis.UniversalClient) RateLimiterBackend {
	return redis_rate.NewLimiter(client)
}

// RateLimitPlan defines the allowed request rate and burst for a named client plan.
type RateLimitPlan struct {
	Rate  int // requests per second
	Burst int // maximum burst size
}

// RedisRateLimiterConfig holds the configuration for the rate-limiter middleware.
// Backend must be provided by the caller — Redis is never created internally.
type RedisRateLimiterConfig struct {
	// Backend is the rate-limiting implementation. Use NewRedisBackend to build
	// one from a redis.UniversalClient.
	Backend RateLimiterBackend

	// Plans maps API keys to specific rate-limit plans. Only keys listed here get
	// their own bucket; any other X-API-Key is limited by client IP.
	Plans map[string]RateLimitPlan

	// IsKnownAPIKey optionally accepts keys outside Plans; they get their own
	// bucket with the Default plan. Unaccepted keys are limited by client IP.
	IsKnownAPIKey func(key string) bool

	// Default is the plan applied to requests whose API key is absent or has no
	// matching entry in Plans.
	Default RateLimitPlan

	// KeyPrefix namespaces rate-limit keys in Redis. Defaults to "rl".
	KeyPrefix string

	// FailClosed rejects requests with 503 while the backend fails. The default
	// (false) lets them through; either way the failure is logged, at most
	// once per backendErrorLogInterval.
	FailClosed bool
}

// backendErrorLogInterval bounds how often a failing backend is logged, so an
// outage does not turn every request into a log line.
const backendErrorLogInterval = 10 * time.Second

// validate rejects plans that would block or bypass every request.
func (cfg RedisRateLimiterConfig) validate() error {
	if cfg.Backend == nil {
		return errors.New("netx: rate limiter backend is required")
	}
	if err := cfg.Default.validate(); err != nil {
		return fmt.Errorf("netx: rate limiter default plan: %w", err)
	}
	for key, plan := range cfg.Plans {
		if err := plan.validate(); err != nil {
			return fmt.Errorf("netx: rate limiter plan %s: %w", apiKeyFingerprint(key), err)
		}
	}
	return nil
}

func (p RateLimitPlan) validate() error {
	if p.Rate <= 0 || p.Burst <= 0 {
		return fmt.Errorf("rate and burst must be > 0 (rate=%d burst=%d)", p.Rate, p.Burst)
	}
	return nil
}

// NewRedisRateLimiter returns a Middleware that enforces per-client rate limits
// using a sliding-window algorithm backed by the provided RateLimiterBackend.
//
// Standard rate-limit response headers (X-RateLimit-Limit, X-RateLimit-Remaining,
// X-RateLimit-Reset) are written on every response. Retry-After is added only
// when the limit is exceeded. Requests without a known API key are bucketed by
// client IP (IPv6 by /64). An invalid cfg (nil Backend, a plan with Rate or
// Burst <= 0) panics: it is a wiring error, like an invalid CIDR.
func NewRedisRateLimiter(cfg RedisRateLimiterConfig) Middleware {
	if err := cfg.validate(); err != nil {
		panic(err)
	}
	keyPrefix := cfg.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "rl"
	}
	var lastErrLog atomic.Int64 // unix nanos of the last backend error log
	var suppressed atomic.Int64

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			plan, key := cfg.bucket(r, keyPrefix)

			res, err := cfg.Backend.Allow(ctx, key, redis_rate.Limit{
				Rate:   plan.Rate,
				Burst:  plan.Burst,
				Period: time.Second,
			})
			if err != nil {
				logBackendError(r, err, cfg.FailClosed, &lastErrLog, &suppressed)
				if cfg.FailClosed {
					w.Header().Set("Retry-After", "1")
					http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			setRateLimitHeaders(w, plan, res)

			if res.Allowed == 0 {
				LogRateLimit(r)
				retryAfter := int(res.RetryAfter/time.Second) + 1
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// bucket picks the plan and backend key for r: a known API key gets its own
// bucket, anything else is limited by client IP under the Default plan.
func (cfg RedisRateLimiterConfig) bucket(r *http.Request, keyPrefix string) (RateLimitPlan, string) {
	apiKey := extractAPIKey(r)
	plan, known := cfg.Plans[apiKey]
	if !known {
		plan = cfg.Default
		known = cfg.IsKnownAPIKey != nil && cfg.IsKnownAPIKey(apiKey)
	}
	if apiKey == "" || !known {
		return cfg.Default, keyPrefix + ":" + rateLimitSubject(r)
	}
	return plan, keyPrefix + ":key:" + apiKeyFingerprint(apiKey)
}

// setRateLimitHeaders writes standard rate-limit headers so clients can
// implement backoff without guessing at window boundaries.
func setRateLimitHeaders(w http.ResponseWriter, plan RateLimitPlan, res *redis_rate.Result) {
	reset := time.Now().Add(res.ResetAfter).Unix()
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", plan.Rate))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", res.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
}

// extractAPIKey returns the value of the X-API-Key header, or an empty string
// when the header is absent.
func extractAPIKey(r *http.Request) string {
	return r.Header.Get("X-API-Key")
}

// logBackendError logs a rate-limiter backend failure at most once per
// backendErrorLogInterval, reporting how many failures were not logged.
func logBackendError(r *http.Request, err error, failClosed bool, last, suppressed *atomic.Int64) {
	now := time.Now().UnixNano()
	prev := last.Load()
	if now-prev < int64(backendErrorLogInterval) || !last.CompareAndSwap(prev, now) {
		suppressed.Add(1)
		return
	}
	logging.FromContext(r.Context()).Error("rate limiter backend failed",
		slog.Any("error", err),
		slog.Bool("fail_closed", failClosed),
		slog.Int64("suppressed", suppressed.Swap(0)),
	)
}

package netx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
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

	// Plans maps API keys to specific rate-limit plans. When an incoming request
	// carries an X-API-Key that matches an entry here, that plan is applied.
	Plans map[string]RateLimitPlan

	// Default is the plan applied to requests whose API key is absent or has no
	// matching entry in Plans.
	Default RateLimitPlan

	// KeyPrefix namespaces rate-limit keys in Redis. Defaults to "rl".
	KeyPrefix string
}

// NewRedisRateLimiter returns a Middleware that enforces per-client rate limits
// using a sliding-window algorithm backed by the provided RateLimiterBackend.
//
// Standard rate-limit response headers (X-RateLimit-Limit, X-RateLimit-Remaining,
// X-RateLimit-Reset) are written on every response. Retry-After is added only
// when the limit is exceeded.
func NewRedisRateLimiter(cfg RedisRateLimiterConfig) Middleware {
	keyPrefix := cfg.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "rl"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			apiKey := extractAPIKey(r)
			clientID := apiKey
			if clientID == "" {
				clientID = extractIP(r)
			}

			plan, ok := cfg.Plans[apiKey]
			if !ok {
				plan = cfg.Default
			}

			key := keyPrefix + ":" + clientID

			res, err := cfg.Backend.Allow(ctx, key, redis_rate.Limit{
				Rate:   plan.Rate,
				Burst:  plan.Burst,
				Period: time.Second,
			})
			if err != nil {
				// Fail-open: do not block traffic when the backend is unavailable.
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

// extractIP resolves the real client IP address. It prefers the leftmost entry
// in X-Forwarded-For (set by trusted reverse proxies) and falls back to
// RemoteAddr when the header is absent.
func extractIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

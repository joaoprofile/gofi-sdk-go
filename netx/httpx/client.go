package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/time/rate"
)

const (
	ErrServiceNotAvailable   = "service not available"
	ErrResponseWithEmptyBody = "response returned with empty body and %d status code"
	ErrConfigNotProvided     = "error config not provided"
)

const (
	defaultTimeout    = 5 * time.Second // 5 segundos de timeout padrão
	defaultRetries    = 5               // 5 tentativas de repetição padrão
	defaultRetrySleep = 2 * time.Second // 2 segundos de espera entre tentativas de repetição padrão
	defaultRateLimit  = 10              // request per second
)

// Client limits applied when the matching field is <= 0.
const (
	// DefaultMaxResponseBytes caps a decoded success body.
	DefaultMaxResponseBytes int64 = 10 << 20
	// DefaultMaxRetryAfter caps the wait a server can impose via Retry-After.
	DefaultMaxRetryAfter = 30 * time.Second
	// maxErrorBodyBytes is how much of an error body is kept in HttpError.Err.
	maxErrorBodyBytes = 64 << 10
	// maxBufferedRequestBytes is how much of an io.Reader body is buffered so
	// it can be retried and signed; larger bodies stream without retries.
	maxBufferedRequestBytes = 1 << 20
	// maxRedirects matches net/http's default.
	maxRedirects = 10
)

// ErrRedirectBlocked is returned when a server redirects to another scheme or
// host: following it would resend custom credential headers (X-API-Key,
// X-Amz-Security-Token, ...) to a third party. Same-origin redirects and the
// http→https upgrade on the same host are followed.
var ErrRedirectBlocked = errors.New("httpx: redirect to a different origin blocked")

// sameOriginRedirect is the http.Client CheckRedirect installed by NewClient.
func sameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("httpx: stopped after %d redirects", maxRedirects)
	}
	from, to := via[0].URL, req.URL
	upgrade := from.Scheme == "http" && to.Scheme == "https"
	if !strings.EqualFold(from.Hostname(), to.Hostname()) ||
		(to.Scheme != from.Scheme && !upgrade) ||
		(to.Port() != from.Port() && !upgrade) {
		return fmt.Errorf("%w: %s -> %s://%s", ErrRedirectBlocked, from.Host, to.Scheme, to.Host)
	}
	return nil
}

type HttpClientConfig struct {
	Name       string
	BaseURL    string
	Timeout    time.Duration
	Retries    uint8
	RetrySleep time.Duration
	RateLimit  int
	// DisableRetryOn429 stops the retry loop from swallowing a 429: the status is
	// returned to the caller as-is, immediately. Retries on 5xx and network errors
	// are unaffected. Set it when an external rate limiter owns the pacing —
	// retrying in place holds the worker for the whole backoff and issues requests
	// the limiter never authorized, which fights the limiter instead of helping it.
	DisableRetryOn429 bool
	// MaxResponseBytes caps a success body; larger ones fail with
	// ErrResponseTooLarge. <= 0 uses DefaultMaxResponseBytes.
	MaxResponseBytes int64
	// MaxRetryAfter caps the Retry-After wait of a 429. <= 0 uses
	// DefaultMaxRetryAfter.
	MaxRetryAfter time.Duration
}

type HttpClient struct {
	Name              string
	BaseURL           string
	Retries           uint8
	RetrySleep        time.Duration
	DisableRetryOn429 bool
	Client            *http.Client
	RateLimit         int
	// MaxResponseBytes and MaxRetryAfter: see HttpClientConfig.
	MaxResponseBytes int64
	MaxRetryAfter    time.Duration
	limiter          *rate.Limiter
	limiterOnce      sync.Once
}

func (c *HttpClient) rateLimiter() *rate.Limiter {
	c.limiterOnce.Do(func() {
		if c.limiter != nil {
			return
		}
		limit := c.RateLimit
		if limit <= 0 {
			limit = defaultRateLimit
		}
		c.limiter = rate.NewLimiter(rate.Limit(limit), limit)
	})
	return c.limiter
}

func NewClient(config *HttpClientConfig) (*HttpClient, error) {
	if config == nil {
		return nil, errors.New(ErrConfigNotProvided)
	}

	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}

	if config.Retries == 0 {
		config.Retries = defaultRetries
	}

	if config.RetrySleep == 0 {
		config.RetrySleep = defaultRetrySleep
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 200
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	client := &http.Client{
		Timeout:       config.Timeout,
		CheckRedirect: sameOriginRedirect,
		// Propagates traceparent and records client spans/metrics.
		Transport: otelhttp.NewTransport(transport),
	}

	if config.RateLimit == 0 {
		config.RateLimit = defaultRateLimit
	}

	return &HttpClient{
		Name:              config.Name,
		BaseURL:           config.BaseURL,
		Retries:           config.Retries,
		RetrySleep:        config.RetrySleep,
		DisableRetryOn429: config.DisableRetryOn429,
		Client:            client,
		RateLimit:         config.RateLimit,
		MaxResponseBytes:  config.MaxResponseBytes,
		MaxRetryAfter:     config.MaxRetryAfter,
		limiter:           rate.NewLimiter(rate.Limit(config.RateLimit), config.RateLimit),
	}, nil
}

package netx

import (
	"errors"
	"net/http"
	"sync"
	"time"

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
}

type HttpClient struct {
	Name              string
	BaseURL           string
	Retries           uint8
	RetrySleep        time.Duration
	DisableRetryOn429 bool
	Client            *http.Client
	RateLimit         int
	limiter           *rate.Limiter
	limiterOnce       sync.Once
	cb                any
	// *circuitbreaker.CircuitBreaker
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
		Timeout:   config.Timeout,
		Transport: transport,
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
		limiter:           rate.NewLimiter(rate.Limit(config.RateLimit), config.RateLimit),
	}, nil
}

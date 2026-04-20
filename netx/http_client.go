package netx

import (
	"errors"
	"net/http"
	"time"
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
}

type HttpClient struct {
	Name            string
	BaseURL         string
	Retries         uint8
	RetrySleep      time.Duration
	Client          *http.Client
	RateLimit       int
	LastRequestTime time.Time
	cb              any
	// *circuitbreaker.CircuitBreaker
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

	client := &http.Client{
		Timeout: config.Timeout,
	}

	if config.RateLimit == 0 {
		config.RateLimit = defaultRateLimit
	}

	return &HttpClient{
		Name:       config.Name,
		BaseURL:    config.BaseURL,
		Retries:    config.Retries,
		RetrySleep: config.RetrySleep,
		Client:     client,
		RateLimit:  config.RateLimit,
	}, nil
}

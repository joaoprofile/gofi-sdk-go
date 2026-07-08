package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"errors"

	"github.com/joaoprofile/gofi/base/common"
)

type RequestBodyResult struct {
	Reader   io.Reader
	JSONData []byte
	Err      error
}

var (
	ErrUnsupportedBodyType = errors.New("unsupported body type")
	ErrTooManyRequests     = errors.New("too many requests")
	ErrResponseIsNil       = errors.New("response is nil")
)

var (
	ErrFailedToCreateRequest         = "failed to create request: %w"
	ErrSignatureRequest              = "signature request error: %w"
	ErrOnExecuteRequest              = "failed to execute request: %w"
	ErrRequestFailed                 = "request failed: %w"
	ErrFailedToReadResponseBody      = "failed to read response body: %w"
	ErrFailedToUnmarshalResponseBody = "failed to unmarshal response body: %w"
	ErrUnexpectedStatusCode          = "unexpected status code: %d | body: %v"
	ErrRequestExceededToStatus429    = "request exceeded retries due to status 429"
	ErrRateLimiting                  = "rate limiting error: %w"
	ErrUnexpectedDuringRetries       = "unexpected error during retries"
)

type RequestBody interface{}

type Request[T any] struct {
	Ctx        context.Context
	Client     *HttpClient
	HttpMethod string
	Url        string
	Headers    map[string]string
	Body       RequestBody
	Retries    int
	retrySleep time.Duration
	signature  Signature
	// ResponseHeaders carrega os headers da última resposta bem-sucedida após
	// Execute(). Permite ao caller ler metadados de transporte (ex.: rate limit
	// devolvido pelo provider) sem o SDK conhecer a semântica de nenhum header
	// específico. Nil até Execute() rodar com sucesso.
	ResponseHeaders http.Header
}

func NewRequest[T any](ctx context.Context, client *HttpClient, method, path string) *Request[T] {
	return &Request[T]{
		Ctx:        ctx,
		Client:     client,
		HttpMethod: method,
		Url:        client.BaseURL + path,
		Headers:    make(map[string]string),
		Retries:    int(client.Retries),
		retrySleep: client.RetrySleep,
	}
}

func (r *Request[T]) SetHeader(key, value string) {
	if _, exists := r.Headers[key]; !exists {
		r.Headers[key] = value
	}
}

func (r *Request[T]) SetSignature(signature Signature) {
	r.signature = signature
}

func (r *Request[T]) SetBody(body interface{}) {
	r.Body = body
}
func (r *Request[T]) prepareRequestBody() *RequestBodyResult {
	var reqBody io.Reader
	var jsonData []byte

	if r.Body == nil || r.HttpMethod == http.MethodGet {
		return &RequestBodyResult{nil, nil, nil}
	}

	switch v := r.Body.(type) {
	case url.Values:
		// Keep the encoded form in jsonData so executeWithRetries can rebuild the
		// reader on retry — a one-shot strings.Reader is consumed on attempt 0 and
		// would send an empty body (e.g. losing grant_type on an OAuth refresh retry).
		jsonData = []byte(v.Encode())
		reqBody = bytes.NewReader(jsonData)
	case *bytes.Buffer:
		jsonData = v.Bytes()
		reqBody = bytes.NewReader(jsonData)
	case io.Reader:
		reqBody = v
	default:
		var err error
		jsonData, err = json.Marshal(v)
		if err != nil {
			return &RequestBodyResult{nil, nil, ErrUnsupportedBodyType}
		}
		reqBody = bytes.NewReader(jsonData)
	}

	return &RequestBodyResult{reqBody, jsonData, nil}
}

func (r *Request[T]) setRequestHeaders(req *http.Request) {
	for key, value := range r.Headers {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}

	switch r.Body.(type) {
	case url.Values:
		if req.Header.Get(common.CONTENT_TYPE) == "" {
			req.Header.Set(common.CONTENT_TYPE, common.APPLICATION_URL_ENCODED)
		}
	case map[string]interface{}, interface{}:
		if req.Header.Get(common.CONTENT_TYPE) == "" {
			req.Header.Set(common.CONTENT_TYPE, common.APPLICATION_JSON)
		}
		if req.Header.Get(common.ACCEPT) == "" {
			req.Header.Set(common.ACCEPT, common.APPLICATION_JSON)
		}
	}
}

func (r *Request[T]) handleRateLimiting() error {
	ctx := r.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return r.Client.rateLimiter().Wait(ctx)
}

func (r *Request[T]) readResponseBody(resp *http.Response) (*T, error) {
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if !strings.HasPrefix(resp.Header.Get(common.CONTENT_TYPE), common.APPLICATION_JSON) {
		io.Copy(io.Discard, resp.Body)
		return nil, nil
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf(ErrFailedToUnmarshalResponseBody, err)
	}

	return &result, nil
}

func (r *Request[T]) createHttpRequest(bodyResult *RequestBodyResult) (*http.Request, error) {
	req, err := http.NewRequestWithContext(r.Ctx, r.HttpMethod, r.Url, bodyResult.Reader)
	if err != nil {
		return nil, fmt.Errorf(ErrFailedToCreateRequest, err)
	}

	if r.signature != nil {
		req, err = r.signature.Sign(req, bodyResult.JSONData)
		if err != nil {
			return nil, fmt.Errorf(ErrSignatureRequest, err)
		}
	}

	r.setRequestHeaders(req)
	return req, nil
}

func (r *Request[T]) executeWithRetries(bodyResult *RequestBodyResult) (*http.Response, error) {
	currentSleep := r.retrySleep

	for attempt := 0; attempt <= r.Retries; attempt++ {
		if bodyResult.JSONData != nil {
			bodyResult.Reader = bytes.NewReader(bodyResult.JSONData)
		}
		if err := r.handleRateLimiting(); err != nil {
			return nil, fmt.Errorf(ErrRateLimiting, err)
		}

		req, err := r.createHttpRequest(bodyResult)
		if err != nil {
			return nil, err
		}

		resp, err := r.Client.Client.Do(req)
		if err != nil {
			if attempt == r.Retries {
				statusCode := http.StatusServiceUnavailable
				if resp != nil {
					statusCode = resp.StatusCode
				}
				return nil, r.handleAPIError(resp, statusCode, err.Error())
			}
			fmt.Printf("Retrying due to network error (attempt %d/%d): %s\n", attempt+1, r.Retries, err.Error())
			time.Sleep(currentSleep)
			continue
		}

		if resp == nil {
			return nil, ErrResponseIsNil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == r.Retries {
				return nil, r.handleAPIError(resp, http.StatusRequestTimeout, ErrRequestExceededToStatus429)
			}
			r.handleRetryAfterHeader(resp, &currentSleep)
			resp.Body.Close()
			fmt.Printf("Retrying due to status 429 (attempt %d/%d): sleep for %s\n", attempt+1, r.Retries, currentSleep)
			time.Sleep(currentSleep)
			continue
		}

		if isClientError(resp.StatusCode) {
			return nil, r.handleClientError(resp)
		}

		if resp.StatusCode == http.StatusInternalServerError {
			if attempt == r.Retries {
				return nil, r.handleAPIError(resp, resp.StatusCode, http.StatusText(resp.StatusCode))
			}
			resp.Body.Close()
			fmt.Printf("Retrying due to status %d (attempt %d/%d)\n", resp.StatusCode, attempt+1, r.Retries)
			time.Sleep(currentSleep)
			continue
		}

		return resp, nil
	}

	return nil, errors.New(ErrUnexpectedDuringRetries)
}

func (r *Request[T]) handleAPIError(resp *http.Response, statusCode int, message string) error {
	if resp != nil {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return NewAPIError(statusCode, message, errors.New(string(bodyBytes)))
	}
	return NewAPIError(statusCode, message, errors.New("unexpected nil response"))
}

func (r *Request[T]) handleClientError(resp *http.Response) error {
	return r.handleAPIError(resp, resp.StatusCode, http.StatusText(resp.StatusCode))
}

func isClientError(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500
}

func (r *Request[T]) incrementRetrySleep(currentSleep time.Duration) time.Duration {
	return currentSleep + 5*time.Second
}

func (r *Request[T]) handleRetryAfterHeader(resp *http.Response, currentSleep *time.Duration) {
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if retryDuration, parseErr := time.ParseDuration(retryAfter + "s"); parseErr == nil {
			time.Sleep(retryDuration)
			return
		}
	}
	*currentSleep = r.incrementRetrySleep(*currentSleep)
}

func (r *Request[T]) Execute() (*T, error) {
	bodyResult := r.prepareRequestBody()
	if bodyResult.Err != nil {
		return nil, bodyResult.Err
	}

	resp, err := r.executeWithRetries(bodyResult)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	r.ResponseHeaders = resp.Header
	return r.readResponseBody(resp)
}

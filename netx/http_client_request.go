package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	"errors"

	"github.com/joaoprofile/gofi-sdk-go/base/common"
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
	// ErrResponseTooLarge is returned when a success body exceeds
	// HttpClient.MaxResponseBytes.
	ErrResponseTooLarge = errors.New("netx: response body too large")
	// ErrURLOutsideBase is returned when the request path would change the
	// scheme, host or base path of HttpClient.BaseURL (e.g. "@evil.com").
	ErrURLOutsideBase = errors.New("netx: request URL escapes the client base URL")
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
	ErrRateLimitedRetryDisabled      = "request rate limited (retry on 429 disabled)"
	ErrRateLimiting                  = "rate limiting error: %w"
	ErrUnexpectedDuringRetries       = "unexpected error during retries"
)

type RequestBody any

type Request[T any] struct {
	Ctx        context.Context
	Client     *HttpClient
	HttpMethod string
	Url        string
	Headers    map[string]string
	Body       RequestBody
	Retries    int
	retrySleep time.Duration
	// disableRetryOn429 surfaces the 429 to the caller instead of retrying it,
	// so an external rate limiter stays in control of the pacing.
	disableRetryOn429 bool
	signature         Signature
	// ResponseHeaders carries the headers of the last response Execute() saw —
	// on success, and also on a 429 surfaced by DisableRetryOn429. Lets the caller
	// read transport metadata (a provider-returned rate limit, for instance)
	// without the SDK knowing the semantics of any particular header. Nil until
	// Execute() has run.
	ResponseHeaders http.Header
	// urlErr is set by NewRequest when path escapes the base URL.
	urlErr error
}

// NewRequest targets client.BaseURL + path. A path that would leave the base
// URL (another scheme, host, userinfo or a "../" above the base path) makes
// Execute fail with ErrURLOutsideBase.
func NewRequest[T any](ctx context.Context, client *HttpClient, method, path string) *Request[T] {
	u, err := joinBaseURL(client.BaseURL, path)
	return &Request[T]{
		Ctx:        ctx,
		Client:     client,
		HttpMethod: method,
		Url:        u,
		Headers:    make(map[string]string),
		Retries:    int(client.Retries),
		retrySleep: client.RetrySleep,
		urlErr:     err,

		disableRetryOn429: client.DisableRetryOn429,
	}
}

// joinBaseURL appends path (which may carry a query) to base and checks the
// result keeps the base scheme, host and path prefix. An empty base accepts
// path as an absolute URL.
func joinBaseURL(base, path string) (string, error) {
	joined := base + path
	if base == "" {
		return joined, nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return joined, err
	}
	u, err := url.Parse(joined)
	if err != nil {
		return joined, err
	}
	// Decoded paths, so %2e%2e and %2f cannot hide a dot-segment.
	basePath := strings.TrimSuffix(b.Path, "/")
	cleaned := pathpkg.Clean("/" + u.Path)
	if u.Scheme != b.Scheme || u.Host != b.Host || u.User.String() != b.User.String() ||
		(cleaned != basePath && !strings.HasPrefix(cleaned, basePath+"/")) {
		return joined, fmt.Errorf("%w: %q", ErrURLOutsideBase, path)
	}
	return joined, nil
}

func (r *Request[T]) SetHeader(key, value string) {
	if _, exists := r.Headers[key]; !exists {
		r.Headers[key] = value
	}
}

func (r *Request[T]) SetSignature(signature Signature) {
	r.signature = signature
}

func (r *Request[T]) SetBody(body any) {
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
		// Buffer small streams so retries resend them and signers hash them;
		// larger ones stream once (see executeWithRetries).
		prefix, err := io.ReadAll(io.LimitReader(v, maxBufferedRequestBytes+1))
		if err != nil {
			return &RequestBodyResult{nil, nil, fmt.Errorf("netx: read request body: %w", err)}
		}
		closer, _ := v.(io.Closer)
		if len(prefix) <= maxBufferedRequestBytes {
			if closer != nil {
				_ = closer.Close() // net/http closed request bodies after sending
			}
			jsonData = prefix
			reqBody = bytes.NewReader(jsonData)
			break
		}
		stream := io.MultiReader(bytes.NewReader(prefix), v)
		if closer != nil {
			reqBody = struct {
				io.Reader
				io.Closer
			}{stream, closer}
		} else {
			reqBody = stream
		}
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
	case map[string]any, any:
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
		// Drain a bounded amount so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil, nil
	}

	var result T
	body := &cappedReader{r: resp.Body, n: r.maxResponseBytes()}
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		if errors.Is(err, ErrResponseTooLarge) {
			return nil, fmt.Errorf("%w (limit %d bytes)", ErrResponseTooLarge, r.maxResponseBytes())
		}
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf(ErrFailedToUnmarshalResponseBody, err)
	}

	return &result, nil
}

// cappedReader fails with ErrResponseTooLarge once more than n bytes are read.
type cappedReader struct {
	r io.Reader
	n int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.n < 0 {
		return 0, ErrResponseTooLarge
	}
	if int64(len(p)) > c.n+1 {
		p = p[:c.n+1]
	}
	n, err := c.r.Read(p)
	c.n -= int64(n)
	if c.n < 0 {
		return n, ErrResponseTooLarge
	}
	return n, err
}

func (r *Request[T]) maxResponseBytes() int64 {
	if r.Client != nil && r.Client.MaxResponseBytes > 0 {
		return r.Client.MaxResponseBytes
	}
	return DefaultMaxResponseBytes
}

func (r *Request[T]) maxRetryAfter() time.Duration {
	if r.Client != nil && r.Client.MaxRetryAfter > 0 {
		return r.Client.MaxRetryAfter
	}
	return DefaultMaxRetryAfter
}

func (r *Request[T]) createHttpRequest(bodyResult *RequestBodyResult) (*http.Request, error) {
	if r.urlErr != nil {
		return nil, fmt.Errorf(ErrFailedToCreateRequest, r.urlErr)
	}
	req, err := http.NewRequestWithContext(r.context(), r.HttpMethod, r.Url, bodyResult.Reader)
	if err != nil {
		return nil, fmt.Errorf(ErrFailedToCreateRequest, err)
	}

	// Headers go first so the signature covers them.
	r.setRequestHeaders(req)

	if r.signature != nil {
		req, err = r.signature.Sign(req, bodyResult.JSONData)
		if err != nil {
			return nil, fmt.Errorf(ErrSignatureRequest, err)
		}
	}
	return req, nil
}

func (r *Request[T]) executeWithRetries(bodyResult *RequestBodyResult) (*http.Response, error) {
	currentSleep := r.retrySleep

	// A streamed body cannot be replayed: a retry would send it empty.
	if bodyResult.Reader != nil && bodyResult.JSONData == nil && r.Retries > 0 {
		slog.DebugContext(r.context(), "netx: retries disabled for a streamed request body")
		r.Retries = 0
	}

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
			if errors.Is(err, ErrRedirectBlocked) {
				return nil, fmt.Errorf(ErrOnExecuteRequest, err)
			}
			if attempt == r.Retries || !r.retrySafe() {
				statusCode := http.StatusServiceUnavailable
				if resp != nil {
					statusCode = resp.StatusCode
				}
				return nil, r.handleAPIError(resp, statusCode, err.Error())
			}
			slog.WarnContext(r.context(), "netx: retrying after network error",
				slog.Int("attempt", attempt+1), slog.Int("retries", int(r.Retries)), slog.Any("error", err))
			if err := sleepCtx(r.context(), withJitter(currentSleep)); err != nil {
				return nil, err
			}
			continue
		}

		if resp == nil {
			return nil, ErrResponseIsNil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if r.disableRetryOn429 {
				r.ResponseHeaders = resp.Header
				return nil, r.handleAPIError(resp, http.StatusTooManyRequests, ErrRateLimitedRetryDisabled)
			}
			if attempt == r.Retries {
				return nil, r.handleAPIError(resp, http.StatusRequestTimeout, ErrRequestExceededToStatus429)
			}
			fromHeader := r.handleRetryAfterHeader(resp, &currentSleep)
			resp.Body.Close()
			slog.WarnContext(r.context(), "netx: retrying after 429",
				slog.Int("attempt", attempt+1), slog.Int("retries", int(r.Retries)), slog.Duration("sleep", currentSleep))
			wait := currentSleep
			if !fromHeader {
				wait = withJitter(wait) // Retry-After is a floor, never shortened
			}
			if err := sleepCtx(r.context(), wait); err != nil {
				return nil, err
			}
			continue
		}

		if isClientError(resp.StatusCode) {
			return nil, r.handleClientError(resp)
		}

		if resp.StatusCode >= http.StatusInternalServerError {
			if !isRetryableStatus(resp.StatusCode) || attempt == r.Retries || !r.retrySafe() {
				return nil, r.handleAPIError(resp, resp.StatusCode, http.StatusText(resp.StatusCode))
			}
			resp.Body.Close()
			slog.WarnContext(r.context(), "netx: retrying after server error",
				slog.Int("status", resp.StatusCode), slog.Int("attempt", attempt+1), slog.Int("retries", int(r.Retries)))
			if err := sleepCtx(r.context(), withJitter(currentSleep)); err != nil {
				return nil, err
			}
			continue
		}

		return resp, nil
	}

	return nil, errors.New(ErrUnexpectedDuringRetries)
}

func (r *Request[T]) handleAPIError(resp *http.Response, statusCode int, message string) error {
	if resp != nil {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
		body := string(bodyBytes)
		if len(bodyBytes) > maxErrorBodyBytes {
			body = string(bodyBytes[:maxErrorBodyBytes]) + "...[truncated]"
		}
		return NewAPIError(statusCode, message, errors.New(body))
	}
	return NewAPIError(statusCode, message, errors.New("unexpected nil response"))
}

func (r *Request[T]) handleClientError(resp *http.Response) error {
	return r.handleAPIError(resp, resp.StatusCode, http.StatusText(resp.StatusCode))
}

// retrySafe reports whether repeating the request after a network error or 5xx
// cannot duplicate side effects: idempotent methods, or an Idempotency-Key.
// A 429 is always retried because the server did not process the request.
func (r *Request[T]) retrySafe() bool {
	switch r.HttpMethod {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete, http.MethodTrace:
		return true
	}
	for k, v := range r.Headers {
		if strings.EqualFold(k, "Idempotency-Key") && v != "" {
			return true
		}
	}
	return false
}

// withJitter spreads retries of many clients across [d/2, d].
func withJitter(d time.Duration) time.Duration {
	if d <= 1 {
		return d
	}
	return d/2 + rand.N(d/2) // #nosec G404 -- backoff jitter, not a secret
}

// isRetryableStatus reports server errors that are usually transient.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (r *Request[T]) context() context.Context {
	if r.Ctx == nil {
		return context.Background()
	}
	return r.Ctx
}

// sleepCtx waits for d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func isClientError(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500
}

func (r *Request[T]) incrementRetrySleep(currentSleep time.Duration) time.Duration {
	return currentSleep + 5*time.Second
}

// handleRetryAfterHeader sets the next wait from Retry-After, capped at
// MaxRetryAfter, and reports whether the header was used; without a valid
// header the wait grows by 5s.
func (r *Request[T]) handleRetryAfterHeader(resp *http.Response, currentSleep *time.Duration) bool {
	if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
		*currentSleep = min(d, r.maxRetryAfter())
		return true
	}
	*currentSleep = r.incrementRetrySleep(*currentSleep)
	return false
}

// parseRetryAfter reads delay-seconds or an HTTP-date (RFC 9110 §10.2.3).
// Dates in the past give 0; anything else is invalid.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(time.Duration(math.MaxInt64)/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
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

package netx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/common"
	"github.com/stretchr/testify/assert"
)

// helpers

func newTestClient(server *httptest.Server, retries uint8, retrySleep time.Duration) *HttpClient {
	return &HttpClient{
		Client:     server.Client(),
		BaseURL:    server.URL,
		Retries:    retries,
		RetrySleep: retrySleep,
		RateLimit:  1000,
	}
}

type mockSignature struct {
	signCalled bool
	signErr    error
}

func (m *mockSignature) Sign(req *http.Request, body []byte) (*http.Request, error) {
	m.signCalled = true
	return req, m.signErr
}

// NewRequest

func TestNewRequest(t *testing.T) {
	client, _ := NewClient(&HttpClientConfig{
		Name:       "Bridge",
		BaseURL:    "http://localhost:8080",
		Retries:    3,
		RetrySleep: time.Duration(100),
	})

	req := NewRequest[any](context.Background(), client, "GET", "/test")

	assert.Equal(t, context.Background(), req.Ctx)
	assert.Equal(t, client, req.Client)
	assert.Equal(t, "GET", req.HttpMethod)
	assert.Equal(t, "http://localhost:8080/test", req.Url)
	assert.Equal(t, 3, req.Retries)
	assert.Equal(t, time.Duration(100), req.retrySleep)
	assert.NotNil(t, req.Headers)
}

// SetHeader─

func TestSetHeader_SetsValue(t *testing.T) {
	req := &Request[string]{Headers: make(map[string]string)}
	req.SetHeader("Content-Type", "application/json")
	assert.Equal(t, "application/json", req.Headers["Content-Type"])
}

func TestSetHeader_DoesNotOverrideExisting(t *testing.T) {
	req := &Request[string]{Headers: map[string]string{"Authorization": "Bearer old"}}
	req.SetHeader("Authorization", "Bearer new")
	assert.Equal(t, "Bearer old", req.Headers["Authorization"])
}

// SetBody / SetSignature

func TestSetBody(t *testing.T) {
	req := &Request[string]{}
	body := map[string]string{"key": "value"}
	req.SetBody(body)
	assert.NotNil(t, req.Body)
	b, ok := req.Body.(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "value", b["key"])
}

func TestSetSignature(t *testing.T) {
	req := &Request[string]{}
	sig := &mockSignature{}
	req.SetSignature(sig)
	assert.Equal(t, sig, req.signature)
}

func TestSetSignature_CalledOnExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	sig := &mockSignature{}
	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodGet, "/test")
	req.SetSignature(sig)
	_, err := req.Execute()

	assert.NoError(t, err)
	assert.True(t, sig.signCalled)
}

// prepareRequestBody

func TestPrepareRequestBody_NilBody(t *testing.T) {
	req := &Request[string]{HttpMethod: http.MethodPost, Body: nil}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)
	assert.Nil(t, result.Reader)
	assert.Nil(t, result.JSONData)
}

func TestPrepareRequestBody_GetIgnoresBody(t *testing.T) {
	req := &Request[string]{
		HttpMethod: http.MethodGet,
		Body:       map[string]string{"key": "value"},
	}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)
	assert.Nil(t, result.Reader)
	assert.Nil(t, result.JSONData)
}

func TestPrepareRequestBody_JSON(t *testing.T) {
	req := &Request[string]{
		HttpMethod: http.MethodPost,
		Body:       map[string]string{"key": "value"},
	}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)
	assert.NotNil(t, result.Reader)

	buf := new(bytes.Buffer)
	buf.ReadFrom(result.Reader)
	assert.Equal(t, `{"key":"value"}`, buf.String())
}

func TestPrepareRequestBody_URLValues(t *testing.T) {
	values := url.Values{"key": []string{"value"}}
	req := &Request[string]{HttpMethod: http.MethodPost, Body: values}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)
	assert.NotNil(t, result.Reader)

	buf := new(bytes.Buffer)
	buf.ReadFrom(result.Reader)
	assert.Equal(t, "key=value", buf.String())
}

func TestPrepareRequestBody_BytesBuffer(t *testing.T) {
	req := &Request[string]{
		HttpMethod: http.MethodPost,
		Body:       bytes.NewBufferString("raw data"),
	}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)

	buf := new(bytes.Buffer)
	buf.ReadFrom(result.Reader)
	assert.Equal(t, "raw data", buf.String())
}

func TestPrepareRequestBody_IOReader(t *testing.T) {
	req := &Request[string]{
		HttpMethod: http.MethodPost,
		Body:       strings.NewReader("stream data"),
	}
	result := req.prepareRequestBody()
	assert.Nil(t, result.Err)

	buf := new(bytes.Buffer)
	buf.ReadFrom(result.Reader)
	assert.Equal(t, "stream data", buf.String())
}

// setRequestHeaders─

func TestSetRequestHeaders_URLEncoded(t *testing.T) {
	req := &Request[string]{
		Headers: make(map[string]string),
		Body:    url.Values{"key": []string{"value"}},
	}
	httpReq, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	req.setRequestHeaders(httpReq)
	assert.Equal(t, common.APPLICATION_URL_ENCODED, httpReq.Header.Get(common.CONTENT_TYPE))
}

func TestSetRequestHeaders_JSON(t *testing.T) {
	req := &Request[string]{
		Headers: make(map[string]string),
		Body:    map[string]string{"key": "value"},
	}
	httpReq, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	req.setRequestHeaders(httpReq)
	assert.Equal(t, common.APPLICATION_JSON, httpReq.Header.Get(common.CONTENT_TYPE))
	assert.Equal(t, common.APPLICATION_JSON, httpReq.Header.Get(common.ACCEPT))
}

func TestSetRequestHeaders_DoesNotOverrideExistingContentType(t *testing.T) {
	req := &Request[string]{
		Headers: map[string]string{common.CONTENT_TYPE: "text/plain"},
		Body:    map[string]string{"key": "value"},
	}
	httpReq, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	req.setRequestHeaders(httpReq)
	assert.Equal(t, "text/plain", httpReq.Header.Get(common.CONTENT_TYPE))
}

// readResponseBody

func TestReadResponseBody_NoContent(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}
	result, err := (&Request[map[string]string]{}).readResponseBody(resp)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestReadResponseBody_NonJSON(t *testing.T) {
	header := make(http.Header)
	header.Set(common.CONTENT_TYPE, "text/plain")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("plain text")),
		Header:     header,
	}
	result, err := (&Request[map[string]string]{}).readResponseBody(resp)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestReadResponseBody_ValidJSON(t *testing.T) {
	header := make(http.Header)
	header.Set(common.CONTENT_TYPE, common.APPLICATION_JSON)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"message":"hello"}`)),
		Header:     header,
	}
	result, err := (&Request[map[string]string]{}).readResponseBody(resp)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "hello", (*result)["message"])
}

func TestReadResponseBody_InvalidJSON(t *testing.T) {
	header := make(http.Header)
	header.Set(common.CONTENT_TYPE, common.APPLICATION_JSON)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("not json")),
		Header:     header,
	}
	result, err := (&Request[map[string]string]{}).readResponseBody(resp)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// A 2xx with Content-Type: application/json but an EMPTY body is a success with
// no content, not an unmarshal failure. Decoding an empty stream yields io.EOF —
// it must be treated as (nil, nil), the same as 204. Guards against a provider
// (e.g. Meli DELETE) that answers 200 + application/json + empty body being
// misread as a transient error and driving false retries.
func TestReadResponseBody_EmptyBodyWithJSONContentType(t *testing.T) {
	header := make(http.Header)
	header.Set(common.CONTENT_TYPE, common.APPLICATION_JSON)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     header,
	}
	result, err := (&Request[any]{}).readResponseBody(resp)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// isClientError─

func TestIsClientError(t *testing.T) {
	assert.True(t, isClientError(400))
	assert.True(t, isClientError(404))
	assert.True(t, isClientError(422))
	assert.True(t, isClientError(499))
	assert.False(t, isClientError(399))
	assert.False(t, isClientError(500))
	assert.False(t, isClientError(200))
}

// handleRateLimiting

func TestHandleRateLimiting_SleepsWhenTooFast(t *testing.T) {
	client := &HttpClient{RateLimit: 2}
	req := &Request[string]{Ctx: context.Background(), Client: client}

	// Drain the initial burst (2 tokens at RateLimit=2) so the next Wait must
	// block until the bucket refills (~500ms at 2 req/s).
	assert.NoError(t, req.handleRateLimiting())
	assert.NoError(t, req.handleRateLimiting())

	start := time.Now()
	err := req.handleRateLimiting()
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 400*time.Millisecond)
}

func TestHandleRateLimiting_RespectsContextCancellation(t *testing.T) {
	client := &HttpClient{RateLimit: 1}
	// Drain the initial token so the next Wait must block until refill.
	warmup := &Request[string]{Ctx: context.Background(), Client: client}
	assert.NoError(t, warmup.handleRateLimiting())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := &Request[string]{Ctx: ctx, Client: client}

	start := time.Now()
	err := req.handleRateLimiting()
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "should return when ctx is cancelled, not wait for token refill")
}

// handleRateLimiting — concurrency / data race coverage
//
// Reproduces the bug from the spike: prior to this fix, `LastRequestTime` was
// read and written from many goroutines without synchronization. Running this
// test under `go test -race` would flag a DATA RACE. With the rate.Limiter
// migration the access is thread-safe.
func TestHandleRateLimiting_ConcurrentCallers_NoDataRace(t *testing.T) {
	client := &HttpClient{RateLimit: 1000} // high limit so the test stays fast
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := &Request[string]{Ctx: context.Background(), Client: client}
			_ = req.handleRateLimiting()
		}()
	}
	wg.Wait()
}

// Verifies the limiter actually enforces the configured rate when many
// goroutines pile in at once. With RateLimit=10 (burst=10) and 20 callers,
// the first 10 are absorbed by the burst and the remaining 10 refill at
// 100ms each — the slowest caller must wait ~1s.
func TestHandleRateLimiting_ConcurrentCallers_RespectsLimit(t *testing.T) {
	client := &HttpClient{RateLimit: 10}
	const goroutines = 20

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := &Request[string]{Ctx: context.Background(), Client: client}
			_ = req.handleRateLimiting()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// Allow some scheduler jitter on the lower bound.
	assert.GreaterOrEqual(t, elapsed, 800*time.Millisecond, "post-burst callers should serialize across ~1s at 10 req/s")
	assert.Less(t, elapsed, 2*time.Second, "should not take dramatically longer than the theoretical window")
}

// handleRetryAfterHeader

func TestHandleRetryAfterHeader_WithHeader_ReturnEarlyWithoutIncrements(t *testing.T) {
	req := &Request[string]{Client: &HttpClient{}}
	resp := &http.Response{
		Header: http.Header{"Retry-After": []string{"0"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}
	currentSleep := 2 * time.Second
	req.handleRetryAfterHeader(resp, &currentSleep)
	// Header present → returns early, currentSleep NOT incremented
	assert.Equal(t, 2*time.Second, currentSleep)
}

func TestHandleRetryAfterHeader_WithoutHeader_IncrementsBy5s(t *testing.T) {
	req := &Request[string]{Client: &HttpClient{}}
	resp := &http.Response{
		Header: make(http.Header),
		Body:   io.NopCloser(strings.NewReader("")),
	}
	currentSleep := 2 * time.Second
	req.handleRetryAfterHeader(resp, &currentSleep)
	assert.Equal(t, 7*time.Second, currentSleep)
}

// Execute (integration with httptest)

func TestExecute_Success_JSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.Equal(t, common.APPLICATION_JSON, r.Header.Get(common.CONTENT_TYPE))

		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "executed"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), "POST", "/test")
	req.SetHeader("Authorization", "Bearer token")
	req.SetBody(map[string]any{"key": "value"})

	resp, err := req.Execute()
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "executed", (*resp)["message"])
}

func TestExecute_ExposesResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.Header().Set("X-Custom-Quota", "0.5")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"ok"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), "GET", "/test")
	assert.Nil(t, req.ResponseHeaders) // nil antes do Execute

	_, err := req.Execute()
	assert.NoError(t, err)
	assert.NotNil(t, req.ResponseHeaders)
	assert.Equal(t, "0.5", req.ResponseHeaders.Get("X-Custom-Quota"))
}

func TestExecute_Success_GetRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "success"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "success", (*result)["message"])
}

func TestExecute_NoContentResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodDelete, "/test")
	result, err := req.Execute()
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// Reproduces the Meli promotion exit: DELETE answered with 200 +
// Content-Type: application/json + empty body. Before the fix this surfaced as
// "failed to unmarshal response body: EOF", got a fabricated 500 from FromError,
// and drove 5 false transient retries. It must now succeed with (nil, nil).
func TestExecute_EmptyJSONBody_TreatedAsNoContent(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req := NewRequest[any](context.Background(), newTestClient(server, 3, time.Millisecond), http.MethodDelete, "/seller-promotions/items/MLB123")
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.Nil(t, result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "empty 200 body must not trigger retries")
}

func TestExecute_ClientError_NoRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 3, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "4xx should not trigger retries")
}

func TestExecute_500_RetryThenSuccess(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "ok", (*result)["result"])
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestExecute_500_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "internal error")

	// A real 5xx must surface as a typed *HttpError carrying the actual status,
	// so callers can distinguish a genuine marketplace 500 from a client-side
	// error (decode/marshal) that has no HTTP status at all.
	var httpErr *HttpError
	assert.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusInternalServerError, httpErr.Status)
}

func TestExecute_429_RetryThenSuccess(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestExecute_429_MaxRetriesExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodGet, "/test")
	result, err := req.Execute()

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestExecute_BodySentCorrectlyOnRetry(t *testing.T) {
	var lastBody string
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		lastBody = string(bodyBytes)
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodPost, "/test")
	req.SetBody(map[string]string{"key": "value"})
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, `{"key":"value"}`, lastBody, "body must be fully present on every retry attempt")
}

// Execute — network error retry (non-last attempt then success)

// roundTripFunc is a lightweight http.RoundTripper backed by a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExecute_NetworkError_RetryThenSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"retried"}`))
	}))
	defer server.Close()

	var attempts int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return nil, errors.New("connection refused")
		}
		return server.Client().Transport.RoundTrip(req)
	})

	client := &HttpClient{
		Client:     &http.Client{Transport: transport},
		BaseURL:    server.URL,
		Retries:    2,
		RetrySleep: time.Millisecond,
		RateLimit:  1000,
	}

	req := NewRequest[map[string]string](context.Background(), client, http.MethodGet, "/test")
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "retried", (*result)["result"])
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

// Execute — unmarshalable body triggers prepareRequestBody error path

func TestExecute_UnmarshalableBody_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodPost, "/test")
	// A channel cannot be marshalled to JSON — triggers ErrUnsupportedBodyType.
	req.SetBody(make(chan int))

	result, err := req.Execute()

	assert.ErrorIs(t, err, ErrUnsupportedBodyType)
	assert.Nil(t, result)
}

// createHttpRequest — invalid URL triggers http.NewRequestWithContext error

func TestExecute_InvalidURL_ReturnsError(t *testing.T) {
	client := &HttpClient{
		Client:     &http.Client{},
		BaseURL:    "://not-valid",
		Retries:    0,
		RetrySleep: time.Millisecond,
		RateLimit:  1000,
	}

	req := NewRequest[map[string]string](context.Background(), client, http.MethodGet, "")
	result, err := req.Execute()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create request")
	assert.Nil(t, result)
}

// prepareRequestBody — unmarshalable default case

func TestPrepareRequestBody_UnmarshalableType_ReturnsErrUnsupportedBodyType(t *testing.T) {
	req := &Request[string]{
		HttpMethod: http.MethodPost,
		Body:       make(chan int), // json.Marshal fails for channels
	}
	result := req.prepareRequestBody()
	assert.ErrorIs(t, result.Err, ErrUnsupportedBodyType)
}

// incrementRetrySleep

func TestIncrementRetrySleep(t *testing.T) {
	req := &Request[string]{}
	assert.Equal(t, 7*time.Second, req.incrementRetrySleep(2*time.Second))
	assert.Equal(t, 5*time.Second, req.incrementRetrySleep(0))
}

// handleAPIError

func TestHandleAPIError_NilResponse(t *testing.T) {
	req := &Request[string]{}
	err := req.handleAPIError(nil, http.StatusServiceUnavailable, "service down")
	httpErr, ok := err.(*HttpError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, httpErr.Status)
	assert.Equal(t, "service down", httpErr.Message)
	assert.Contains(t, httpErr.Err, "unexpected nil response")
}

func TestHandleAPIError_WithResponse(t *testing.T) {
	req := &Request[string]{}
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"detail":"bad input"}`)),
		Header:     make(http.Header),
	}
	err := req.handleAPIError(resp, http.StatusBadRequest, "bad request")
	httpErr, ok := err.(*HttpError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Status)
	assert.Equal(t, `{"detail":"bad input"}`, httpErr.Err)
}

// handleClientError

func TestHandleClientError_Returns4xxError(t *testing.T) {
	req := &Request[string]{}
	resp := &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Body:       io.NopCloser(strings.NewReader(`{"error":"unprocessable"}`)),
		Header:     make(http.Header),
	}
	err := req.handleClientError(resp)
	httpErr, ok := err.(*HttpError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, httpErr.Status)
	assert.Equal(t, http.StatusText(http.StatusUnprocessableEntity), httpErr.Message)
}

// Execute — signature error

func TestExecute_SignatureError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sig := &mockSignature{signErr: errors.New("sign failed")}
	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 1, time.Millisecond), http.MethodGet, "/test")
	req.SetSignature(sig)
	result, err := req.Execute()

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "sign failed")
}

// Execute — bytes.Buffer body retained across retries

func TestExecute_BytesBufferBody_RetainedOnRetry(t *testing.T) {
	var lastBody string
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		lastBody = string(bodyBytes)
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodPost, "/test")
	req.SetBody(bytes.NewBufferString("raw buffer data"))
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
	assert.Equal(t, "raw buffer data", lastBody, "bytes.Buffer body must be present on every retry attempt")
}

// Execute — url.Values (form) body retained across retries

func TestExecute_URLValuesBody_RetainedOnRetry(t *testing.T) {
	var lastBody string
	var contentType string
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		lastBody = string(bodyBytes)
		contentType = r.Header.Get(common.CONTENT_TYPE)
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", common.APPLICATION_JSON)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "rt-123")

	req := NewRequest[map[string]string](context.Background(), newTestClient(server, 2, time.Millisecond), http.MethodPost, "/oauth/token")
	req.SetBody(form)
	result, err := req.Execute()

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
	assert.Equal(t, common.APPLICATION_URL_ENCODED, contentType)
	assert.Contains(t, lastBody, "grant_type=refresh_token", "form body must survive the retry, not be sent empty")
	assert.Contains(t, lastBody, "refresh_token=rt-123", "form body must survive the retry, not be sent empty")
}

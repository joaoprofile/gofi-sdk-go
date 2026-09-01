package netx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type retryProbe struct{ calls atomic.Int32 }

func (p *retryProbe) server(t *testing.T, status int, header map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.calls.Add(1)
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"throttled"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newRetryTestClient(t *testing.T, baseURL string, disable bool) *HttpClient {
	t.Helper()
	client, err := NewClient(&HttpClientConfig{
		Name:              "RetryTest",
		BaseURL:           baseURL,
		Timeout:           2 * time.Second,
		Retries:           1,
		RetrySleep:        1 * time.Second,
		RateLimit:         100,
		DisableRetryOn429: disable,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

type retryPayload struct {
	Message string `json:"message"`
}

// Retrying a 429 in place holds the worker for the whole backoff (retrySleep + 5s)
// and issues a second request the caller's rate limiter never authorized. With
// DisableRetryOn429 the status reaches the caller on the first response.
func TestExecute_DisableRetryOn429_ReturnsFirstResponse(t *testing.T) {
	p := &retryProbe{}
	srv := p.server(t, http.StatusTooManyRequests, nil)
	client := newRetryTestClient(t, srv.URL, true)

	start := time.Now()
	req := NewRequest[retryPayload](context.Background(), client, http.MethodGet, "/x")
	_, err := req.Execute()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for status 429")
	}
	if got := FromError(err).Status; got != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d — the caller must see the real 429, not the exhausted-retry remap",
			got, http.StatusTooManyRequests)
	}
	if got := p.calls.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 — a retry spends quota the limiter did not grant", got)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %s, want no backoff sleep", elapsed)
	}
}

// The 429 response may carry provider pacing metadata. Surfacing the headers lets
// the caller honor it without the SDK interpreting any particular header.
func TestExecute_DisableRetryOn429_ExposesResponseHeaders(t *testing.T) {
	p := &retryProbe{}
	srv := p.server(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"})
	client := newRetryTestClient(t, srv.URL, true)

	req := NewRequest[retryPayload](context.Background(), client, http.MethodGet, "/x")
	if _, err := req.Execute(); err == nil {
		t.Fatal("expected an error for status 429")
	}
	if got := req.ResponseHeaders.Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
}

// DisableRetryOn429 is scoped to 429 only: a 5xx is a provider fault the caller
// cannot pace around, so the retry budget still applies to it.
func TestExecute_DisableRetryOn429_StillRetriesOn5xx(t *testing.T) {
	p := &retryProbe{}
	srv := p.server(t, http.StatusInternalServerError, nil)
	client := newRetryTestClient(t, srv.URL, true)

	req := NewRequest[retryPayload](context.Background(), client, http.MethodGet, "/x")
	if _, err := req.Execute(); err == nil {
		t.Fatal("expected an error for status 500")
	}
	if got := p.calls.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (initial + one retry)", got)
	}
}

// Default behaviour is preserved: without the flag a 429 is retried and the
// exhausted budget is remapped to 408.
func TestExecute_DefaultRetriesOn429_RemapsTo408(t *testing.T) {
	if testing.Short() {
		t.Skip("backoff sleep is retrySleep + 5s")
	}
	p := &retryProbe{}
	srv := p.server(t, http.StatusTooManyRequests, nil)
	client := newRetryTestClient(t, srv.URL, false)

	req := NewRequest[retryPayload](context.Background(), client, http.MethodGet, "/x")
	_, err := req.Execute()

	if err == nil {
		t.Fatal("expected an error for status 429")
	}
	if got := FromError(err).Status; got != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", got, http.StatusRequestTimeout)
	}
	if got := p.calls.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (initial + one retry)", got)
	}
}

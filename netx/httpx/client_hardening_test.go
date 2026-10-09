package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHardenedClient(t *testing.T, baseURL string) *HttpClient {
	t.Helper()
	c, err := NewClient(&HttpClientConfig{BaseURL: baseURL, Retries: 2, RetrySleep: time.Millisecond, RateLimit: 1000})
	require.NoError(t, err)
	return c
}

// ── Redirects ────────────────────────────────────────────────────────────────

func TestClient_CrossHostRedirectBlocked(t *testing.T) {
	var leaked atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("X-API-Key") != "")
	}))
	defer evil.Close()
	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// 127.0.0.1 vs localhost: same machine, different host.
		http.Redirect(w, r, strings.Replace(evil.URL, "127.0.0.1", "localhost", 1)+"/steal", http.StatusFound)
	}))
	defer origin.Close()

	req := NewRequest[map[string]any](t.Context(), newHardenedClient(t, origin.URL), http.MethodGet, "/data")
	req.SetHeader("X-API-Key", "secret")
	_, err := req.Execute()

	require.ErrorIs(t, err, ErrRedirectBlocked)
	assert.False(t, leaked.Load(), "credentials must not reach the redirect target")
	assert.Equal(t, int32(1), hits.Load(), "a blocked redirect is not retried")
}

func TestClient_SameHostRedirectFollowed(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		gotKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	req := NewRequest[map[string]bool](t.Context(), newHardenedClient(t, srv.URL), http.MethodGet, "/old")
	req.SetHeader("X-API-Key", "secret")
	out, err := req.Execute()
	require.NoError(t, err)
	assert.True(t, (*out)["ok"])
	assert.Equal(t, "secret", gotKey)
}

func TestSameOriginRedirect(t *testing.T) {
	mk := func(u string) *http.Request { r, _ := http.NewRequest(http.MethodGet, u, nil); return r }
	tests := []struct {
		from, to string
		ok       bool
	}{
		{"https://api.x.com/a", "https://api.x.com/b", true},
		{"https://api.x.com/a", "https://API.X.COM/b", true},
		{"http://api.x.com/a", "https://api.x.com/b", true},
		{"https://api.x.com/a", "http://api.x.com/b", false},
		{"https://api.x.com/a", "https://evil.com/b", false},
		{"https://api.x.com/a", "https://api.x.com:8443/b", false},
	}
	for _, tt := range tests {
		err := sameOriginRedirect(mk(tt.to), []*http.Request{mk(tt.from)})
		assert.Equal(t, tt.ok, err == nil, "%s -> %s: %v", tt.from, tt.to, err)
	}
	via := make([]*http.Request, maxRedirects)
	for i := range via {
		via[i] = mk("https://api.x.com/")
	}
	assert.Error(t, sameOriginRedirect(mk("https://api.x.com/"), via))
}

// ── URL building ─────────────────────────────────────────────────────────────

func TestJoinBaseURL(t *testing.T) {
	tests := []struct {
		base, path, want string
		ok               bool
	}{
		{"https://api.x.com", "/items?x=1", "https://api.x.com/items?x=1", true},
		{"https://api.x.com/v1", "/items", "https://api.x.com/v1/items", true},
		{"https://api.x.com/v1", "", "https://api.x.com/v1", true},
		{"https://api.x.com/v1", "?q=1", "https://api.x.com/v1?q=1", true},
		{"", "https://other.com/x", "https://other.com/x", true},
		{"https://api.x.com", "@evil.com/x", "", false},
		{"https://api.x.com", ".evil.com/x", "", false},
		{"https://api.x.com", ":1234/x", "", false},
		{"https://api.x.com/v1", "/../admin", "", false},
		{"https://api.x.com/v1", "/%2e%2e/admin", "", false},
		{"https://api.x.com/v1", "/a%2f..%2f..%2fadmin", "", false},
		{"https://api.x.com/v1", "/a/../b", "", true},
		{"https://api.x.com/v1", "extra", "", false},
		{"://bad", "/x", "", false},
	}
	for _, tt := range tests {
		got, err := joinBaseURL(tt.base, tt.path)
		if !tt.ok {
			assert.Error(t, err, "%s + %s", tt.base, tt.path)
			continue
		}
		require.NoError(t, err, "%s + %s", tt.base, tt.path)
		if tt.want != "" {
			assert.Equal(t, tt.want, got)
		}
	}
}

func TestExecute_PathEscapingBaseFails(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	_, err := NewRequest[any](t.Context(), newHardenedClient(t, srv.URL), http.MethodGet, "@evil.com/x").Execute()
	require.ErrorIs(t, err, ErrURLOutsideBase)
	assert.Zero(t, hits.Load())
}

// ── Retry-After ──────────────────────────────────────────────────────────────

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"5", 5 * time.Second, true},
		{" 0 ", 0, true},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, true},
		{"99999999999999999", time.Duration(1<<63 - 1), true},
	}
	for _, tt := range tests {
		got, ok := parseRetryAfter(tt.in, now)
		assert.Equal(t, tt.ok, ok, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestHandleRetryAfterHeader_Capped(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"3600"}}}
	sleep := time.Second

	req := &Request[any]{Client: &HttpClient{}}
	assert.True(t, req.handleRetryAfterHeader(resp, &sleep))
	assert.Equal(t, DefaultMaxRetryAfter, sleep)

	req = &Request[any]{Client: &HttpClient{MaxRetryAfter: 2 * time.Second}}
	assert.True(t, req.handleRetryAfterHeader(resp, &sleep))
	assert.Equal(t, 2*time.Second, sleep)
}

// ── Response size ────────────────────────────────────────────────────────────

func TestExecute_ResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"v":"`+strings.Repeat("a", 64)+`"}`)
	}))
	defer srv.Close()

	c := newHardenedClient(t, srv.URL)
	c.MaxResponseBytes = 32
	_, err := NewRequest[map[string]string](t.Context(), c, http.MethodGet, "/").Execute()
	require.ErrorIs(t, err, ErrResponseTooLarge)

	c.MaxResponseBytes = 1 << 10
	out, err := NewRequest[map[string]string](t.Context(), c, http.MethodGet, "/").Execute()
	require.NoError(t, err)
	assert.Len(t, (*out)["v"], 64)
}

func TestCappedReader_ExactLimit(t *testing.T) {
	b, err := io.ReadAll(&cappedReader{r: strings.NewReader("abcd"), n: 4})
	require.NoError(t, err)
	assert.Equal(t, "abcd", string(b))

	_, err = io.ReadAll(&cappedReader{r: strings.NewReader("abcde"), n: 4})
	assert.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestExecute_ErrorBodyTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, strings.Repeat("e", 1<<20))
	}))
	defer srv.Close()

	_, err := NewRequest[any](t.Context(), newHardenedClient(t, srv.URL), http.MethodGet, "/").Execute()
	var he *HttpError
	require.True(t, errors.As(err, &he))
	assert.LessOrEqual(t, len(he.Err), maxErrorBodyBytes+len("...[truncated]"))
	assert.True(t, strings.HasSuffix(he.Err, "...[truncated]"))
}

// ── Request bodies on retry ──────────────────────────────────────────────────

// onceReader is an io.Reader that is neither a Seeker nor one of the types
// net/http knows how to rewind.
type onceReader struct{ r io.Reader }

func (o *onceReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func bodyRecorder(t *testing.T, failFirst int) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n <= failFirst {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), bodies...) }
}

func TestExecute_StreamBodyReplayedOnRetry(t *testing.T) {
	srv, bodies := bodyRecorder(t, 1)
	req := NewRequest[any](t.Context(), newHardenedClient(t, srv.URL), http.MethodPut, "/")
	req.SetBody(&onceReader{strings.NewReader("payload")})
	_, err := req.Execute()
	require.NoError(t, err)
	assert.Equal(t, []string{"payload", "payload"}, bodies())
}

func TestExecute_LargeStreamBodyNotRetried(t *testing.T) {
	srv, bodies := bodyRecorder(t, 1)
	big := strings.Repeat("z", maxBufferedRequestBytes+10)
	req := NewRequest[any](t.Context(), newHardenedClient(t, srv.URL), http.MethodPut, "/")
	req.SetBody(&onceReader{strings.NewReader(big)})
	_, err := req.Execute()
	require.Error(t, err, "the 503 is returned instead of retrying with an empty body")
	got := bodies()
	require.Len(t, got, 1)
	assert.Equal(t, len(big), len(got[0]), "the streamed body is sent whole")
}

// ── Headers before signing ───────────────────────────────────────────────────

type headerCapturingSigner struct{ contentType, custom string }

func (s *headerCapturingSigner) Sign(r *http.Request, _ []byte) (*http.Request, error) {
	s.contentType, s.custom = r.Header.Get("Content-Type"), r.Header.Get("X-Custom")
	return r, nil
}

func TestExecute_HeadersSetBeforeSigning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()

	signer := &headerCapturingSigner{}
	req := NewRequest[any](context.Background(), newHardenedClient(t, srv.URL), http.MethodPost, "/")
	req.SetBody(map[string]int{"a": 1})
	req.SetHeader("X-Custom", "v")
	req.SetSignature(signer)
	_, err := req.Execute()
	require.NoError(t, err)
	assert.Equal(t, "application/json", signer.contentType)
	assert.Equal(t, "v", signer.custom)
}

type closeTracker struct {
	io.Reader
	closed atomic.Bool
}

func (c *closeTracker) Close() error { c.closed.Store(true); return nil }

func TestPrepareRequestBody_ClosesBufferedReadCloser(t *testing.T) {
	body := &closeTracker{Reader: strings.NewReader("x")}
	req := &Request[any]{HttpMethod: http.MethodPost, Body: body}
	res := req.prepareRequestBody()
	require.NoError(t, res.Err)
	assert.Equal(t, []byte("x"), res.JSONData)
	assert.True(t, body.closed.Load())
}

package jwks

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJWKSCache_KeyNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[]}`))
	}))
	defer ts.Close()

	c := NewCache(time.Hour)
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "missingkid")
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
}

func TestJWKSCache_FetchNetworkError(t *testing.T) {
	c := NewCache(time.Hour)
	_, err := c.GetKey(context.Background(), &http.Client{}, "http://127.0.0.1:1", "kid")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestJWKSCache_FetchInvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	c := NewCache(time.Hour)
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "kid")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestJWKSCache_CacheHit(t *testing.T) {
	callCount := 0
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	e := priv.E
	eBytes := []byte{byte(e >> 16), byte(e >> 8), byte(e)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"k1","n":"%s","e":"%s"}]}`,
		base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
		base64.RawURLEncoding.EncodeToString(eBytes))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(jwks))
	}))
	defer ts.Close()

	c := NewCache(time.Hour)
	ctx := context.Background()

	if _, err := c.GetKey(ctx, ts.Client(), ts.URL, "k1"); err != nil {
		t.Fatalf("first GetKey error: %v", err)
	}
	if _, err := c.GetKey(ctx, ts.Client(), ts.URL, "k1"); err != nil {
		t.Fatalf("second GetKey error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 JWKS fetch, got %d", callCount)
	}
}

func TestJWKSCache_SkipsMalformedKeys(t *testing.T) {
	// A JWKS with an unknown key type that gets skipped.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[{"kty":"oct","kid":"k1"}]}`))
	}))
	defer ts.Close()

	c := NewCache(time.Hour)
	_, err := c.GetKey(context.Background(), ts.Client(), ts.URL, "k1")
	if err == nil {
		t.Fatal("expected key not found error after skipping malformed key")
	}
}

// Attack/outage: a 5xx (or empty set) used to replace good keys with an empty list.
func TestJWKSCache_FailedRefreshKeepsPreviousKeys(t *testing.T) {
	_, jwks := generateRSAKey(t)
	srv := newJWKSServer(t, jwks)
	c := NewCache(time.Hour)
	ctx := context.Background()
	if _, err := c.GetKey(ctx, srv.Client(), srv.URL, "key1"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []struct {
		status int
		body   string
	}{{http.StatusInternalServerError, `{"keys":[]}`}, {http.StatusOK, `{"keys":[]}`}} {
		srv.status.Store(int32(bad.status))
		srv.body.Store(bad.body)
		c.mu.Lock()
		c.fetchedAt = time.Now().Add(-2 * time.Hour) // expired: forces a refetch
		c.mu.Unlock()
		if _, err := c.GetKey(ctx, srv.Client(), srv.URL, "key1"); err != nil {
			t.Fatalf("status %d: previous key must stay usable: %v", bad.status, err)
		}
	}
	if err := c.fetch(ctx, srv.Client(), srv.URL); err == nil {
		t.Fatal("fetch must report the failure")
	}
}

func TestJWKSCache_Non200IsError(t *testing.T) {
	_, jwks := generateRSAKey(t)
	srv := newJWKSServer(t, jwks)
	srv.status.Store(http.StatusBadGateway)
	c := NewCache(time.Hour)
	if _, err := c.GetKey(context.Background(), srv.Client(), srv.URL, "key1"); err == nil {
		t.Fatal("a 502 must not be parsed as keys")
	}
}

func TestJWKSCache_BodyIsCapped(t *testing.T) {
	_, jwks := generateRSAKey(t)
	huge := strings.Replace(jwks, `"keys":[`, `"pad":"`+strings.Repeat("a", MaxBody)+`","keys":[`, 1)
	srv := newJWKSServer(t, huge)
	c := NewCache(time.Hour)
	if _, err := c.GetKey(context.Background(), srv.Client(), srv.URL, "key1"); err == nil {
		t.Fatal("a body over 1 MiB must be rejected")
	}
}

// Attack: tokens with random kids must not turn every login into a JWKS fetch.
func TestJWKSCache_UnknownKidRefetchIsRateLimited(t *testing.T) {
	_, jwks := generateRSAKey(t)
	srv := newJWKSServer(t, jwks)
	c := NewCache(time.Hour)
	ctx := context.Background()
	for i := range 5 {
		_, _ = c.GetKey(ctx, srv.Client(), srv.URL, fmt.Sprint("random-", i))
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetches=%d, want 1", n)
	}
}

func TestJWKSCache_ConcurrentFetchesShareOneRequest(t *testing.T) {
	_, jwks := generateRSAKey(t)
	srv := newJWKSServer(t, jwks)
	srv.delay = 50 * time.Millisecond
	c := NewCache(time.Hour)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := c.GetKey(context.Background(), srv.Client(), srv.URL, "key1"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetches=%d, want 1", n)
	}
}

func TestRequireHTTPS(t *testing.T) {
	ok := []string{"https://idp.example/token", "http://127.0.0.1:8080/jwks", "http://localhost/x", "http://[::1]:9/x"}
	bad := []string{"http://idp.example/token", "t", "ftp://idp.example", "https://", "://bad"}
	for _, u := range ok {
		if err := RequireHTTPS(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := RequireHTTPS(u); err == nil {
			t.Errorf("%s must be rejected", u)
		}
	}
}

func TestCache_RefreshReportsUsableKeys(t *testing.T) {
	_, set := generateRSAKey(t)
	srv := newJWKSServer(t, set)
	n, err := NewCache(time.Hour).Refresh(context.Background(), srv.Client(), srv.URL)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want 1 key", n, err)
	}
	srv.body.Store(`{"keys":[{"kty":"oct","kid":"k"}]}`)
	if _, err := NewCache(time.Hour).Refresh(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("a set with only symmetric keys must be rejected")
	}
}

func TestCache_PlainHTTPIsRejected(t *testing.T) {
	_, err := NewCache(time.Hour).GetKey(context.Background(), http.DefaultClient, "http://idp.example/jwks", "k")
	if !errors.Is(err, ErrInsecureEndpoint) {
		t.Fatalf("err=%v, want ErrInsecureEndpoint", err)
	}
}

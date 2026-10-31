package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
	"github.com/joaoprofile/gofi-sdk-go/iam/provider/jwks"
)

// jwksServer serves jwks at /jwks; status and body can be switched at runtime.
type jwksServer struct {
	*httptest.Server
	status atomic.Int32
	body   atomic.Value
	hits   atomic.Int32
	delay  time.Duration
}

func newJWKSServer(t *testing.T, body string) *jwksServer {
	t.Helper()
	s := &jwksServer{}
	s.status.Store(http.StatusOK)
	s.body.Store(body)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		time.Sleep(s.delay)
		w.WriteHeader(int(s.status.Load()))
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}

func signClaims(t *testing.T, claims gojwt.MapClaims) (string, string) {
	t.Helper()
	key, jwks := generateRSAKey(t)
	tok := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "key1"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed, jwks
}

func TestValidateIDToken_TimeClaims(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		claims  gojwt.MapClaims
		wantErr bool
	}{
		{"no exp", gojwt.MapClaims{}, true},
		{"iat far in the future", gojwt.MapClaims{"exp": now.Add(time.Hour).Unix(), "iat": now.Add(10 * time.Minute).Unix()}, true},
		{"iat within the leeway", gojwt.MapClaims{"exp": now.Add(time.Hour).Unix(), "iat": now.Add(30 * time.Second).Unix()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.claims["aud"], tc.claims["sub"], tc.claims["nonce"] = "client1", "u1", testNonce
			srv := newJWKSServer(t, "")
			tc.claims["iss"] = srv.URL
			idToken, jwks := signClaims(t, tc.claims)
			srv.body.Store(jwks)

			p := New("test", Config{IssuerURL: srv.URL, ClientID: "client1", HTTPClient: srv.Client()})
			_, err := p.validateIDToken(context.Background(), &discoveryDoc{JWKSURI: srv.URL}, idToken, testNonce)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestGetDiscovery_ConcurrentRefetchSharesOneRequestAndHonorsCtx(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		<-release
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":"a","token_endpoint":"t","jwks_uri":"j"}`, ts.URL)
	}))
	defer ts.Close()
	p := buildProvider(t, ts.URL, ts.Client())

	// A waiter whose ctx ends returns without waiting for the fetch.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.getDiscovery(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want the caller's deadline", err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := p.getDiscovery(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("discovery fetches=%d, want 1", n)
	}
}

// The client secret and code must never travel over plain http.
func TestExchangeCode_RejectsPlainHTTP(t *testing.T) {
	p := buildProvider(t, "https://example.com", nil)
	_, err := p.exchangeCode(context.Background(), "http://idp.example/token", port.IDPCallbackInput{})
	if !errors.Is(err, ErrInsecureEndpoint) {
		t.Fatalf("err=%v, want ErrInsecureEndpoint", err)
	}
	c := jwks.NewCache(time.Hour)
	if _, err := c.GetKey(context.Background(), http.DefaultClient, "http://idp.example/jwks", "k"); !errors.Is(err, ErrInsecureEndpoint) {
		t.Fatalf("err=%v, want ErrInsecureEndpoint", err)
	}
}

// The token endpoint's error body used to be copied whole into the error.
func TestExchangeCode_ErrorBodyTruncatedAndKeptOutOfMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","debug":"` + strings.Repeat("x", 10_000) + `"}`))
	}))
	defer ts.Close()
	p := buildProvider(t, "https://example.com", ts.Client())

	_, err := p.exchangeCode(context.Background(), ts.URL, port.IDPCallbackInput{})
	var te *TokenEndpointError
	if !errors.As(err, &te) {
		t.Fatalf("err=%v, want *TokenEndpointError", err)
	}
	if te.Status != http.StatusBadRequest || len(te.Body) > maxErrorBody || !strings.Contains(te.Body, "invalid_grant") {
		t.Fatalf("status=%d body len=%d", te.Status, len(te.Body))
	}
	if strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("message must not carry the body: %q", err.Error())
	}
}

func TestRedirectURI_DefaultAndMismatch(t *testing.T) {
	ts := buildDiscoveryServer(t, "/jwks")
	defer ts.Close()
	p := buildProvider(t, ts.URL, ts.Client()) // RedirectURI: http://localhost/callback

	res, err := p.AuthorizationURL(context.Background(), port.IDPAuthInput{State: "s"})
	if err != nil || !strings.Contains(res.URL, "redirect_uri=http%3A%2F%2Flocalhost%2Fcallback") {
		t.Fatalf("configured RedirectURI must be the default: %v %v", res, err)
	}
	_, err = p.AuthorizationURL(context.Background(), port.IDPAuthInput{State: "s", RedirectURI: "https://evil.example/cb"})
	if !errors.Is(err, ErrRedirectURIMismatch) {
		t.Fatalf("err=%v, want ErrRedirectURIMismatch", err)
	}
	_, err = p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State: "s", ExpectedState: "s", CodeVerifier: "v", RedirectURI: "https://evil.example/cb",
	})
	if !errors.Is(err, ErrRedirectURIMismatch) {
		t.Fatalf("err=%v, want ErrRedirectURIMismatch", err)
	}
}

func TestNew_ClockSkewDefaultAndCap(t *testing.T) {
	if got := New("x", Config{}).cfg.ClockSkew; got != defaultClockSkew {
		t.Errorf("default=%v", got)
	}
	if got := New("x", Config{ClockSkew: time.Hour}).cfg.ClockSkew; got != core.MaxClockSkew {
		t.Errorf("capped=%v, want %v", got, core.MaxClockSkew)
	}
}

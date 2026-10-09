package awssign

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	cloudaws "github.com/joaoprofile/gofi-sdk-go/base/cloud/aws"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
)

var _ httpx.Signature = (*Signer)(nil)

func staticConfig(region string) awssdk.Config {
	return awssdk.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("AKIAIOSFODNN7EXAMPLE", "secret", ""),
	}
}

func TestSign_AddsSigV4Headers(t *testing.T) {
	s, err := NewWithConfig(staticConfig("us-east-1"), "")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"k":"v"}`)
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/items", nil)
	if _, err := s.Sign(req, body); err != nil {
		t.Fatal(err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/") ||
		!strings.Contains(auth, "/us-east-1/execute-api/aws4_request") {
		t.Fatalf("Authorization=%q", auth)
	}
	if req.Header.Get("X-Amz-Date") == "" {
		t.Fatal("X-Amz-Date must be set")
	}
}

func TestSign_UsesServiceName(t *testing.T) {
	s, err := NewWithConfig(staticConfig("sa-east-1"), "es")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://search.example.com/", nil)
	if _, err := s.Sign(req, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "/sa-east-1/es/aws4_request") {
		t.Fatalf("Authorization=%q", req.Header.Get("Authorization"))
	}
}

func TestNewWithConfig_Validates(t *testing.T) {
	if _, err := NewWithConfig(awssdk.Config{Credentials: staticConfig("x").Credentials}, ""); err == nil {
		t.Error("missing region must fail")
	}
	if _, err := NewWithConfig(awssdk.Config{Region: "us-east-1"}, ""); err == nil {
		t.Error("missing credentials must fail")
	}
}

func TestNew_StaticKeys(t *testing.T) {
	s, err := New(t.Context(), Config{AWS: cloudaws.Config{Region: "us-east-1", AccessKeyID: "AKID", SecretAccessKey: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/", nil)
	if _, err := s.Sign(req, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "Credential=AKID/") {
		t.Fatalf("Authorization=%q", req.Header.Get("Authorization"))
	}
}

func payloadHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// expectedAuth re-signs a copy of req with the payload body and the signing
// time Sign used, returning the Authorization header SigV4 should produce.
func expectedAuth(t *testing.T, req *http.Request, body []byte) string {
	t.Helper()
	at, err := time.Parse("20060102T150405Z", req.Header.Get("X-Amz-Date"))
	if err != nil {
		t.Fatal(err)
	}
	clone := req.Clone(req.Context())
	clone.Header.Del("Authorization")
	clone.Header.Del("X-Amz-Date")
	creds, _ := staticConfig("us-east-1").Credentials.Retrieve(t.Context())
	if err := v4.NewSigner().SignHTTP(t.Context(), creds, clone, payloadHash(body), ServiceExecuteAPI, "us-east-1", at); err != nil {
		t.Fatal(err)
	}
	return clone.Header.Get("Authorization")
}

func TestSign_NilBodyHashesGetBody(t *testing.T) {
	s, err := NewWithConfig(staticConfig("us-east-1"), "")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"k":"v"}`)
	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/items", bytes.NewReader(body))
	if _, err := s.Sign(req, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := req.Header.Get("Authorization"), expectedAuth(t, req, body); got != want {
		t.Fatalf("signature does not cover the body:\n got %s\nwant %s", got, want)
	}
	if rest, _ := io.ReadAll(req.Body); !bytes.Equal(rest, body) {
		t.Fatalf("Sign consumed the body: %q", rest)
	}
}

func TestSign_StreamingBodyWithoutGetBodyFails(t *testing.T) {
	s, err := NewWithConfig(staticConfig("us-east-1"), "")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/items", io.NopCloser(strings.NewReader("x")))
	if _, err := s.Sign(req, nil); err == nil {
		t.Fatal("a body that cannot be hashed must not be signed as empty")
	}
}

func TestSign_CoversHeadersSetBeforeSigning(t *testing.T) {
	s, err := NewWithConfig(staticConfig("us-east-1"), "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/items", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Custom", "1")
	if _, err := s.Sign(req, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "content-type") || !strings.Contains(auth, "x-custom") {
		t.Fatalf("SignedHeaders miss client headers: %s", auth)
	}
}

type countingProvider struct{ calls atomic.Int32 }

func (c *countingProvider) Retrieve(context.Context) (awssdk.Credentials, error) {
	c.calls.Add(1)
	return awssdk.Credentials{AccessKeyID: "AKID", SecretAccessKey: "s", CanExpire: true, Expires: time.Now().Add(time.Hour)}, nil
}

func TestNewWithConfig_CachesRawProvider(t *testing.T) {
	p := &countingProvider{}
	s, err := NewWithConfig(awssdk.Config{Region: "us-east-1", Credentials: p}, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.Sign(httptest.NewRequest(http.MethodGet, "https://api.example.com/", nil), nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("provider called %d times, want 1 (cached)", n)
	}

	cache := awssdk.NewCredentialsCache(p)
	s, _ = NewWithConfig(awssdk.Config{Region: "us-east-1", Credentials: cache}, "")
	if s.creds != cache {
		t.Fatal("an existing cache must not be wrapped again")
	}
}

package core

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
)

func TestApplyTLS(t *testing.T) {
	tr := http.DefaultTransport.(*http.Transport)
	orig := tr.TLSClientConfig
	t.Cleanup(func() { tr.TLSClientConfig = orig })

	ApplyTLS(&environment.Environment{})
	if c := tr.TLSClientConfig; c != nil && c.InsecureSkipVerify {
		t.Fatal("verification must stay on by default")
	}

	ApplyTLS(&environment.Environment{TLSInsecureSkipVerify: true})
	if c := tr.TLSClientConfig; c == nil || !c.InsecureSkipVerify {
		t.Fatal("TLS_INSECURE_SKIP_VERIFY must disable verification")
	}
}

func TestApplyTLSKeepsExistingSettings(t *testing.T) {
	tr := http.DefaultTransport.(*http.Transport)
	orig := tr.TLSClientConfig
	t.Cleanup(func() { tr.TLSClientConfig = orig })
	tr.TLSClientConfig = &tls.Config{ServerName: "api.internal"}

	ApplyTLS(&environment.Environment{TLSInsecureSkipVerify: true})
	if c := tr.TLSClientConfig; !c.InsecureSkipVerify || c.ServerName != "api.internal" {
		t.Fatalf("TLS config not cloned: %+v", c)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestApplyTLSIgnoresCustomDefaultTransport(t *testing.T) {
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })

	ApplyTLS(&environment.Environment{TLSInsecureSkipVerify: true}) // must not panic
}

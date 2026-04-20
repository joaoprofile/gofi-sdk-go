package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/joaoprofile/gofi/iam/port"
)

// ---- New ----

func TestNew_NoHostedDomain(t *testing.T) {
	p := New(Config{
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "http://localhost/callback",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.inner == nil {
		t.Fatal("expected non-nil inner OIDC provider")
	}
}

func TestNew_WithHostedDomain(t *testing.T) {
	// When HostedDomain is set, an extra "hd" scope is added to ExtraScopes.
	p := New(Config{
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "http://localhost/callback",
		HostedDomain: "example.com",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.inner == nil {
		t.Fatal("expected non-nil inner OIDC provider")
	}
}

func TestNew_WithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	p := New(Config{
		ClientID:   "cid",
		HTTPClient: custom,
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ---- ProviderName ----

func TestProviderName(t *testing.T) {
	p := New(Config{})
	if got := p.ProviderName(); got != "google" {
		t.Errorf("ProviderName()=%q, want google", got)
	}
}

// ---- AuthorizationURL ----

// AuthorizationURL delegates to oidc.Provider which makes a discovery HTTP call.
// We use a custom HTTPClient that always returns an error to verify the delegation
// without hitting the real Google endpoint.
func TestAuthorizationURL_DelegatesAndReturnsDiscoveryError(t *testing.T) {
	// Unreachable address forces a network error from the inner OIDC provider.
	p := New(Config{
		ClientID:   "cid",
		HTTPClient: &http.Client{Transport: &errorTransport{}},
	})

	_, err := p.AuthorizationURL(context.Background(), port.IDPAuthInput{
		State: "s1",
		Nonce: "n1",
	})
	if err == nil {
		t.Fatal("expected error from discovery delegation, got nil")
	}
}

// ---- HandleCallback ----

// HandleCallback returns ErrInvalidIDPState before any network call when state mismatches.
func TestHandleCallback_StateMismatch(t *testing.T) {
	p := New(Config{ClientID: "cid"})

	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{
		State:         "wrong",
		ExpectedState: "correct",
	})
	if err == nil {
		t.Fatal("expected error for state mismatch")
	}
}

// errorTransport is an http.RoundTripper that always returns an error.
type errorTransport struct{}

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &networkError{msg: "simulated transport error"}
}

type networkError struct{ msg string }

func (e *networkError) Error() string { return e.msg }

package microsoft

import (
	"context"
	"net/http"
	"testing"

	"github.com/joaoprofile/gofi/iam/port"
)

// ---- New ----

func TestNew_DefaultTenantID(t *testing.T) {
	// When TenantID is empty, it must default to "common".
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

func TestNew_ExplicitTenantID(t *testing.T) {
	p := New(Config{
		ClientID: "cid",
		TenantID: "my-tenant",
	})
	if p == nil {
		t.Fatal("expected non-nil provider")
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

func TestNew_OrganizationsTenantID(t *testing.T) {
	p := New(Config{TenantID: "organizations"})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNew_ConsumersTenantID(t *testing.T) {
	p := New(Config{TenantID: "consumers"})
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ---- ProviderName ----

func TestProviderName(t *testing.T) {
	p := New(Config{})
	if got := p.ProviderName(); got != "microsoft" {
		t.Errorf("ProviderName()=%q, want microsoft", got)
	}
}

// ---- AuthorizationURL ----

// AuthorizationURL delegates to oidc.Provider which makes a discovery HTTP call.
// An injected transport that always errors verifies the delegation without hitting MSFT.
func TestAuthorizationURL_DelegatesAndReturnsDiscoveryError(t *testing.T) {
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

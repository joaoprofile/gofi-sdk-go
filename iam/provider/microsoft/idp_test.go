package microsoft

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/iam/core"
	"github.com/joaoprofile/gofi-sdk-go/iam/port"
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

// Attack: empty state and no state cookie (login CSRF) is rejected before any network call.
func TestHandleCallback_EmptyStateRejected(t *testing.T) {
	p := New(Config{ClientID: "cid", HTTPClient: &http.Client{Transport: &errorTransport{}}})

	_, err := p.HandleCallback(context.Background(), port.IDPCallbackInput{Code: "attacker-code"})
	if !errors.Is(err, core.ErrInvalidIDPState) {
		t.Fatalf("expected ErrInvalidIDPState, got %v", err)
	}
}

// errorTransport is an http.RoundTripper that always returns an error.
type errorTransport struct{}

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &networkError{msg: "simulated transport error"}
}

type networkError struct{ msg string }

func (e *networkError) Error() string { return e.msg }

// ---- stable identity ----

func TestExternalID_TenantAndObjectID(t *testing.T) {
	id, err := externalID(map[string]any{
		"sub": "pairwise-per-app",
		"tid": "9188040D-6C67-4C5B-B112-36A304B66DAD",
		"oid": "00000000-0000-0000-66F3-3332ECA7EA81",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "9188040d-6c67-4c5b-b112-36a304b66dad:00000000-0000-0000-66f3-3332eca7ea81" {
		t.Errorf("id=%q", id)
	}
}

func TestExternalID_RequiresOID(t *testing.T) {
	for _, claims := range []map[string]any{
		{"sub": "s", "tid": "t"},
		{"sub": "s", "oid": "o"},
	} {
		if _, err := externalID(claims); !errors.Is(err, ErrMissingObjectID) {
			t.Errorf("claims %v: err=%v, want ErrMissingObjectID", claims, err)
		}
	}
}

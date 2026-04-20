package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── NewHostAuthentication ─────────────────────────────────────────────────────

func TestNewHostAuthentication_PopulatesFields(t *testing.T) {
	auth := NewHostAuthentication("key-id", "secret", "us-east-1")

	assert.Equal(t, "key-id", auth.IAMKeyId)
	assert.Equal(t, "secret", auth.IAMSecretKey)
	assert.Equal(t, "us-east-1", auth.Region)
}

func TestNewHostAuthentication_ReturnsNonNil(t *testing.T) {
	auth := NewHostAuthentication("", "", "")
	assert.NotNil(t, auth)
}

// ── NewAwsSigner ──────────────────────────────────────────────────────────────

func TestNewAwsSigner_ReturnsNonNil(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "secret", "us-east-1")
	signer := NewAwsSigner(auth)
	assert.NotNil(t, signer)
}

func TestNewAwsSigner_ImplementsSignatureInterface(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "secret", "us-east-1")
	var _ Signature = NewAwsSigner(auth)
}

// ── AwsSigner.Sign ────────────────────────────────────────────────────────────

func TestAwsSigner_Sign_AddsAuthorizationHeader(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	signer := NewAwsSigner(auth)

	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/items", nil)
	body := []byte(`{"key":"value"}`)

	signed, err := signer.Sign(req, body)
	require.NoError(t, err)

	assert.NotNil(t, signed)
	assert.NotEmpty(t, signed.Header.Get("Authorization"), "Authorization header must be set by AWS signer")
}

func TestAwsSigner_Sign_SetsContentLength(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	signer := NewAwsSigner(auth)

	body := []byte(`{"hello":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/items", nil)

	signed, err := signer.Sign(req, body)
	require.NoError(t, err)

	assert.Equal(t, int64(len(body)), signed.ContentLength)
}

func TestAwsSigner_Sign_ReplacesBody(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	signer := NewAwsSigner(auth)

	body := []byte(`{"id":1}`)
	req := httptest.NewRequest(http.MethodPut, "https://api.example.com/items/1", nil)

	signed, err := signer.Sign(req, body)
	require.NoError(t, err)

	assert.NotNil(t, signed.Body, "body must be set after signing")
}

func TestAwsSigner_Sign_EmptyBody_DoesNotError(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	signer := NewAwsSigner(auth)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/items", nil)

	signed, err := signer.Sign(req, []byte{})
	require.NoError(t, err)
	assert.NotNil(t, signed)
}

func TestAwsSigner_Sign_ReturnsSameRequest(t *testing.T) {
	auth := NewHostAuthentication("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1")
	signer := NewAwsSigner(auth)

	req := httptest.NewRequest(http.MethodGet, "https://api.example.com/", nil)
	signed, err := signer.Sign(req, nil)

	require.NoError(t, err)
	assert.Same(t, req, signed, "Sign must return the same request pointer it received")
}

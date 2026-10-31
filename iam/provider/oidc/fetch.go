package oidc

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/joaoprofile/gofi-sdk-go/iam/provider/jwks"
)

// maxBody caps discovery and token responses.
const maxBody = 1 << 20

// maxErrorBody caps the token endpoint error body kept in TokenEndpointError.
const maxErrorBody = 512

// ErrInsecureEndpoint is returned for a token or JWKS endpoint that is not
// https (plain http is accepted only on loopback, for tests).
var ErrInsecureEndpoint = jwks.ErrInsecureEndpoint

// ErrRedirectURIMismatch is returned when the redirect URI differs from Config.RedirectURI.
var ErrRedirectURIMismatch = errors.New("iam/oidc: redirect_uri does not match the configured RedirectURI")

// TokenEndpointError is a non-200 token response. Error() carries only the
// status, safe to surface; Body (truncated) is for server-side logs only.
type TokenEndpointError struct {
	Status int
	Body   string
}

func (e *TokenEndpointError) Error() string {
	return fmt.Sprintf("iam/oidc: token endpoint returned %d", e.Status)
}

// newTokenEndpointError reads at most maxErrorBody bytes of the error body.
func newTokenEndpointError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return &TokenEndpointError{Status: resp.StatusCode, Body: string(b)}
}

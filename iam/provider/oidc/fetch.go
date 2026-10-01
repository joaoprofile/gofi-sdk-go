package oidc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxBody caps discovery, JWKS and token responses.
const maxBody = 1 << 20

// fetchTimeout bounds a shared fetch even with an HTTPClient without timeout.
const fetchTimeout = 30 * time.Second

// maxErrorBody caps the token endpoint error body kept in TokenEndpointError.
const maxErrorBody = 512

// ErrInsecureEndpoint is returned for a token or JWKS endpoint that is not
// https (plain http is accepted only on loopback, for tests).
var ErrInsecureEndpoint = errors.New("iam/oidc: endpoint must use https")

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

// requireHTTPS accepts https, and http only for loopback hosts.
func requireHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("iam/oidc: invalid endpoint %q: %w", raw, err)
	}
	if u.Scheme == "https" && u.Host != "" {
		return nil
	}
	if u.Scheme == "http" && isLoopback(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInsecureEndpoint, raw)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// flight runs one fetch at a time for concurrent callers (a minimal
// singleflight). The fetch runs detached from the first caller's
// cancellation; each waiter stops on its own ctx.
type flight struct {
	mu  sync.Mutex
	cur *flightCall
}

type flightCall struct {
	done chan struct{}
	err  error
}

func (f *flight) do(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	c := f.cur
	if c == nil {
		c = &flightCall{done: make(chan struct{})}
		f.cur = c
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
		go func() {
			defer cancel()
			c.err = fn(fctx)
			f.mu.Lock()
			f.cur = nil
			f.mu.Unlock()
			close(c.done)
		}()
	}
	f.mu.Unlock()
	select {
	case <-c.done:
		return c.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

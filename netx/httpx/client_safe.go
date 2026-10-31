package httpx

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Safe client limits applied when the matching SafeClientConfig field is zero.
const (
	DefaultSafeClientTimeout      = 10 * time.Second
	DefaultSafeClientMaxRedirects = 3
)

// ErrDestinationBlocked is returned when the address a request would connect
// to is not public: loopback, private, link-local (cloud metadata included),
// CGNAT, multicast, documentation or reserved ranges.
var ErrDestinationBlocked = errors.New("httpx: destination address blocked")

// ErrInsecureScheme is returned for a request (or redirect) that is not https.
var ErrInsecureScheme = errors.New("httpx: only https is allowed")

// SafeClientConfig configures NewSafeClient.
type SafeClientConfig struct {
	// Timeout bounds each request, redirects and body read included.
	// Default: DefaultSafeClientTimeout.
	Timeout time.Duration

	// MaxResponseBytes caps a response body; reading past it fails with
	// ErrResponseTooLarge. Default: DefaultMaxResponseBytes.
	MaxResponseBytes int64

	// MaxRedirects caps same-origin redirects; a redirect to another origin is
	// always blocked (ErrRedirectBlocked). Default: DefaultSafeClientMaxRedirects;
	// a negative value disables redirects.
	MaxRedirects int

	// AllowedNetworks are exceptions to the blocked ranges, for a known
	// internal gateway or for tests. Leave empty for third-party URLs.
	AllowedNetworks []netip.Prefix

	// RootCAs replaces the system pool, for a private CA or for tests.
	RootCAs *x509.CertPool
}

// NewSafeClient returns an http.Client for URLs configured by someone else
// (webhooks, customer APIs, JWKS): https only, no proxy, every connection
// checked after DNS resolution — so a hostname that resolves, or later
// re-resolves, to an internal address is refused — same-origin redirects
// only, and a capped response body. It is the defense against SSRF.
func NewSafeClient(cfg SafeClientConfig) *http.Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultSafeClientTimeout
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if cfg.MaxRedirects == 0 {
		cfg.MaxRedirects = DefaultSafeClientMaxRedirects
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   destinationGuard(cfg.AllowedNetworks),
	}
	transport := &http.Transport{
		Proxy:                 nil, // a proxy would hide the real destination from the guard
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: cfg.RootCAs},
	}

	return &http.Client{
		Timeout:       cfg.Timeout,
		Transport:     &safeTransport{next: transport, maxBody: cfg.MaxResponseBytes},
		CheckRedirect: safeRedirect(cfg.MaxRedirects),
	}
}

// safeTransport refuses non-https requests and caps the response body.
type safeTransport struct {
	next    http.RoundTripper
	maxBody int64
}

func (t *safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w: %s", ErrInsecureScheme, req.URL.Redacted())
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &cappedBody{rc: resp.Body, left: t.maxBody}
	return resp, nil
}

// cappedBody fails with ErrResponseTooLarge instead of silently truncating.
type cappedBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		var probe [1]byte
		if n, _ := b.rc.Read(probe[:]); n > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *cappedBody) Close() error { return b.rc.Close() }

func safeRedirect(max int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if max < 0 || len(via) > max {
			return fmt.Errorf("httpx: stopped after %d redirects", len(via))
		}
		return sameOriginRedirect(req, via)
	}
}

// blockedPrefixes complements the netip predicates with special-purpose
// ranges that are not routable on the internet (RFC 6890 and successors).
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach IPv4 internals
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fec0::/10"),
}

// isBlocked reports whether addr is not a public unicast address.
func isBlocked(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return true // loopback, link-local, multicast, unspecified fall here
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// destinationGuard is the net.Dialer Control hook: it runs for every
// connection with the resolved address, redirects included.
func destinationGuard(allowed []netip.Prefix) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrDestinationBlocked, address)
		}
		addr := ap.Addr().Unmap()
		for _, p := range allowed {
			if p.Contains(addr) {
				return nil
			}
		}
		if isBlocked(addr) {
			return fmt.Errorf("%w: %s", ErrDestinationBlocked, addr)
		}
		return nil
	}
}

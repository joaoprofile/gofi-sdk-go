package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var loopbackNet = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}

func newSafeTLSServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func safeClientFor(srv *httptest.Server, cfg SafeClientConfig) *http.Client {
	cfg.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	return NewSafeClient(cfg)
}

func get(t *testing.T, c *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return c.Do(req)
}

func TestIsBlocked(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.10", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "255.255.255.255", "224.0.0.1", "198.18.0.1",
		"::1", "::", "fe80::1", "fd00:ec2::254", "fc00::1", "::ffff:127.0.0.1",
		"::ffff:169.254.169.254", "64:ff9b::a00:1", "2001:db8::1",
	}
	public := []string{"8.8.8.8", "1.1.1.1", "200.160.2.3", "2606:4700:4700::1111"}
	for _, s := range blocked {
		assert.True(t, isBlocked(netip.MustParseAddr(s)), "%s must be blocked", s)
	}
	for _, s := range public {
		assert.False(t, isBlocked(netip.MustParseAddr(s)), "%s must be allowed", s)
	}
}

// Attack: a third-party URL pointing at the cloud metadata service or an internal host.
func TestSafeClient_BlocksInternalDestinations(t *testing.T) {
	srv := newSafeTLSServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	c := safeClientFor(srv, SafeClientConfig{})

	_, err := get(t, c, srv.URL)
	assert.ErrorIs(t, err, ErrDestinationBlocked)

	// A hostname that resolves to loopback is refused after resolution.
	_, err = get(t, c, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	assert.ErrorIs(t, err, ErrDestinationBlocked)
}

func TestSafeClient_AllowedNetworkReachesTheServer(t *testing.T) {
	srv := newSafeTLSServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	resp, err := get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet}), srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "ok", string(body))
}

func TestSafeClient_RejectsPlainHTTP(t *testing.T) {
	_, err := get(t, NewSafeClient(SafeClientConfig{AllowedNetworks: loopbackNet}), "http://127.0.0.1:1/x")
	assert.ErrorIs(t, err, ErrInsecureScheme)
}

func TestSafeClient_BodyOverTheCapFails(t *testing.T) {
	srv := newSafeTLSServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", 2048))
	})
	resp, err := get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet, MaxResponseBytes: 1024}), srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	assert.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestSafeClient_BodyAtTheCapIsRead(t *testing.T) {
	srv := newSafeTLSServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", 1024))
	})
	resp, err := get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet, MaxResponseBytes: 1024}), srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Len(t, body, 1024)
}

// Attack: credentials in custom headers must not follow a redirect to another origin.
func TestSafeClient_BlocksCrossOriginRedirect(t *testing.T) {
	srv := newSafeTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	})
	_, err := get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet}), srv.URL)
	assert.ErrorIs(t, err, ErrRedirectBlocked)
}

func TestSafeClient_RedirectLimit(t *testing.T) {
	var srv *httptest.Server
	srv = newSafeTLSServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
	})
	_, err := get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet, MaxRedirects: 2}), srv.URL)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrRedirectBlocked))
	assert.Contains(t, err.Error(), "stopped after")

	_, err = get(t, safeClientFor(srv, SafeClientConfig{AllowedNetworks: loopbackNet, MaxRedirects: -1}), srv.URL)
	assert.Contains(t, err.Error(), "stopped after", "a negative MaxRedirects disables redirects")
}

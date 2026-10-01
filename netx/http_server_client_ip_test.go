package netx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func resolvedIP(t *testing.T, trusted []string, remoteAddr string, xff ...string) string {
	t.Helper()
	var got string
	h := ClientIPMiddleware(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = clientIP(r)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for _, v := range xff {
		req.Header.Add("X-Forwarded-For", v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestClientIP(t *testing.T) {
	lb := []string{"10.0.0.0/8"}
	tests := []struct {
		name    string
		trusted []string
		remote  string
		xff     []string
		want    string
	}{
		{"default: public peer", nil, "203.0.113.7:4000", nil, "203.0.113.7"},
		{"default: spoofed XFF from public peer ignored", nil, "203.0.113.7:4000", []string{"1.2.3.4"}, "203.0.113.7"},
		{"default: private peer is not trusted", nil, "10.1.2.3:80", []string{"198.51.100.9"}, "10.1.2.3"},
		{"default: loopback peer is not trusted", nil, "127.0.0.1:80", []string{"198.51.100.9"}, "127.0.0.1"},
		{"empty list trusts no proxy", []string{}, "10.1.2.3:80", []string{"198.51.100.9"}, "10.1.2.3"},
		{"private alias: private LB forwards client", []string{TrustPrivateNetworks}, "10.1.2.3:80", []string{"198.51.100.9"}, "198.51.100.9"},
		{"private alias: client-supplied XFF prefix ignored", []string{"private"}, "10.1.2.3:80", []string{"1.2.3.4, 198.51.100.9"}, "198.51.100.9"},
		{"PrivateNetworks list", PrivateNetworks(), "192.168.1.1:80", []string{"198.51.100.9"}, "198.51.100.9"},
		{"untrusted peer: XFF ignored", lb, "203.0.113.7:4000", []string{"1.2.3.4"}, "203.0.113.7"},
		{"trusted peer: rightmost untrusted hop", lb, "10.0.0.2:80", []string{"1.2.3.4, 198.51.100.9, 10.0.0.5"}, "198.51.100.9"},
		{"trusted peer: multiple XFF lines", lb, "10.0.0.2:80", []string{"1.2.3.4", "198.51.100.9"}, "198.51.100.9"},
		{"trusted peer: garbage hop falls back to peer", lb, "10.0.0.2:80", []string{"1.2.3.4, garbage"}, "10.0.0.2"},
		{"trusted peer: no XFF", lb, "10.0.0.2:80", nil, "10.0.0.2"},
		{"v4-mapped peer", nil, "[::ffff:203.0.113.7]:4000", nil, "203.0.113.7"},
		{"peer without port", nil, "203.0.113.7", nil, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolvedIP(t, tt.trusted, tt.remote, tt.xff...))
		})
	}
}

func TestClientIP_WithoutMiddlewareUsesPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:5000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	assert.Equal(t, "203.0.113.7", clientIP(req))

	// A private peer must not be able to spoof the client IP by default.
	req.RemoteAddr = "192.168.0.1:5000"
	assert.Equal(t, "192.168.0.1", clientIP(req))
}

func TestPrivateNetworks_ReturnsCopy(t *testing.T) {
	PrivateNetworks()[0] = "0.0.0.0/0"
	assert.Equal(t, "10.0.0.0/8", PrivateNetworks()[0])
}

func TestRateLimitSubject(t *testing.T) {
	tests := []struct{ remote, want string }{
		{"203.0.113.7:1", "203.0.113.7"},
		{"[::ffff:203.0.113.7]:1", "203.0.113.7"},
		{"[2001:db8:1:2:aaaa::1]:1", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:bbbb::9]:1", "2001:db8:1:2::/64"},
		{"garbage", "garbage"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remote
		assert.Equal(t, tt.want, rateLimitSubject(req), tt.remote)
	}
}

func TestClientIPMiddleware_InvalidCIDRPanics(t *testing.T) {
	assert.Panics(t, func() { ClientIPMiddleware([]string{"not-a-cidr"}) })
}

func TestAPIKeyAttr_DoesNotLeakKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "super-secret-key")
	attr := apiKeyAttr(req)
	assert.NotContains(t, attr.Value.String(), "super-secret")
	assert.Equal(t, apiKeyFingerprint("super-secret-key"), attr.Value.String())

	assert.Empty(t, apiKeyAttr(httptest.NewRequest(http.MethodGet, "/", nil)).Value.String())
}

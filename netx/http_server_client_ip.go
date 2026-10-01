package netx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientIPKey struct{}

// TrustPrivateNetworks is a TrustedProxies entry that expands to
// PrivateNetworks(), so "private" can be set from an env var.
const TrustPrivateNetworks = "private"

// PrivateNetworks returns the private, loopback, link-local and CGNAT ranges
// in-cluster ingresses and cloud load balancers reach the pod from. Trusting
// them lets any internal peer spoof X-Forwarded-For, so opt in only when every
// such peer is a proxy.
func PrivateNetworks() []string {
	return []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10",
		"127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
	}
}

// ClientIPMiddleware resolves the client IP once per request. X-Forwarded-For is
// honoured only when the TCP peer is in trustedProxies (CIDRs, or
// TrustPrivateNetworks); nil or empty trusts no proxy and uses the TCP peer.
// Invalid CIDRs panic.
func ClientIPMiddleware(trustedProxies []string) Middleware {
	prefixes := mustPrefixes(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := resolveClientIP(r, prefixes); ip.IsValid() {
				r = r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveClientIP walks X-Forwarded-For right to left and returns the first untrusted hop.
func resolveClientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := peerIP(r)
	if !peer.IsValid() || !inPrefixes(peer, trusted) {
		return peer
	}

	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(line, ",")...)
	}
	for _, hop := range slices.Backward(hops) {
		ip, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			return peer
		}
		if ip = ip.Unmap().WithZone(""); !inPrefixes(ip, trusted) {
			return ip
		}
	}
	return peer
}

func mustPrefixes(cidrs []string) []netip.Prefix {
	out, err := parsePrefixes(cidrs)
	if err != nil {
		panic(err)
	}
	return out
}

// parsePrefixes parses CIDRs, expanding TrustPrivateNetworks.
func parsePrefixes(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == TrustPrivateNetworks {
			private, _ := parsePrefixes(PrivateNetworks())
			out = append(out, private...)
			continue
		}
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, fmt.Errorf("netx: trusted proxy: %w", err)
		}
		out = append(out, p)
	}
	return out, nil
}

func peerIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

func inPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// clientAddr returns the IP resolved by ClientIPMiddleware, or the TCP peer.
func clientAddr(r *http.Request) netip.Addr {
	if ip, ok := r.Context().Value(clientIPKey{}).(netip.Addr); ok {
		return ip
	}
	return peerIP(r)
}

// clientIP returns clientAddr as a string, or RemoteAddr when it is unparsable.
func clientIP(r *http.Request) string {
	if ip := clientAddr(r); ip.IsValid() {
		return ip.String()
	}
	return r.RemoteAddr
}

// rateLimitSubject is the client identity used as a rate-limit bucket: the IPv4
// address, or the /64 of an IPv6 address, since one host usually owns a whole
// /64 and could otherwise rotate addresses to dodge the limit.
func rateLimitSubject(r *http.Request) string {
	ip := clientAddr(r)
	if !ip.IsValid() {
		return r.RemoteAddr
	}
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip.String()
}

// apiKeyFingerprint identifies an API key in logs and cache keys without exposing it.
func apiKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func apiKeyAttr(r *http.Request) slog.Attr {
	key := extractAPIKey(r)
	if key == "" {
		return slog.String("api_key", "")
	}
	return slog.String("api_key", apiKeyFingerprint(key))
}

package httpserver

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

// clientAuthModes maps HTTP_TLS_CLIENT_AUTH to tls.ClientAuthType.
var clientAuthModes = map[string]tls.ClientAuthType{
	"none":               tls.NoClientCert,
	"request":            tls.RequestClientCert,
	"require":            tls.RequireAnyClientCert,
	"verify_if_given":    tls.VerifyClientCertIfGiven,
	"require_and_verify": tls.RequireAndVerifyClientCert,
}

// ConfigFromEnv builds the netx.WSConfig fields set by the environment:
//
//   - HTTP_TLS_CERT_FILE / HTTP_TLS_KEY_FILE → TLS (both or neither)
//   - HTTP_TLS_CLIENT_CA_FILE               → TLS.ClientCAFile (mTLS)
//   - HTTP_TLS_CLIENT_AUTH                  → TLS.ClientAuth: none | request |
//     require | verify_if_given | require_and_verify (default
//     require_and_verify with a client CA, none without)
//   - HTTP_TRUSTED_PROXIES                  → TrustedProxies (CSV of CIDRs, or "private")
//   - HTTP_MAX_CONCURRENT                   → StressControl.DefaultMaxConcurrent
//   - HTTP_ALLOWED_ORIGINS (ALLOWED_ORIGINS as fallback) → AllowedOrigins (CSV)
//
// HTTP_RATE_LIMIT_FAIL_CLOSED needs a rate limiter backend, which only code
// can provide, so it is applied by New to the WSConfig.RateLimiter given there.
// Invalid values are returned as errors, never panics.
func ConfigFromEnv(env *environment.Environment) (netx.WSConfig, error) {
	tlsCfg, err := tlsFromEnv(env)
	if err != nil {
		return netx.WSConfig{}, err
	}
	cfg := netx.WSConfig{
		TLS:            tlsCfg,
		TrustedProxies: splitCSV(env.HTTPTrustedProxies),
		AllowedOrigins: splitCSV(cmp.Or(env.HTTPAllowedOrigins, env.AllowedOrigins)),
	}
	switch {
	case env.HTTPMaxConcurrent < 0:
		return netx.WSConfig{}, fmt.Errorf("HTTP_MAX_CONCURRENT=%d: must be >= 0", env.HTTPMaxConcurrent)
	case env.HTTPMaxConcurrent > 0:
		cfg.StressControl = &netx.StressControlConfig{DefaultMaxConcurrent: env.HTTPMaxConcurrent}
	}
	if err := cfg.Validate(); err != nil {
		return netx.WSConfig{}, fmt.Errorf("HTTP_TRUSTED_PROXIES / HTTP_ALLOWED_ORIGINS: %w", err)
	}
	return cfg, nil
}

// tlsFromEnv returns nil when no HTTP_TLS_* variable is set.
func tlsFromEnv(env *environment.Environment) (*netx.TLSConfig, error) {
	cert := strings.TrimSpace(env.HTTPTLSCertFile)
	key := strings.TrimSpace(env.HTTPTLSKeyFile)
	ca := strings.TrimSpace(env.HTTPTLSClientCAFile)
	mode := strings.ToLower(strings.TrimSpace(env.HTTPTLSClientAuth))

	if cert == "" && key == "" {
		if ca != "" || mode != "" {
			return nil, errors.New("HTTP_TLS_CLIENT_CA_FILE / HTTP_TLS_CLIENT_AUTH need HTTP_TLS_CERT_FILE and HTTP_TLS_KEY_FILE")
		}
		return nil, nil
	}
	if cert == "" || key == "" {
		return nil, errors.New("HTTP_TLS_CERT_FILE and HTTP_TLS_KEY_FILE must be set together")
	}
	auth, err := clientAuth(mode, ca != "")
	if err != nil {
		return nil, err
	}
	return &netx.TLSConfig{CertFile: cert, KeyFile: key, ClientCAFile: ca, ClientAuth: auth}, nil
}

// clientAuth parses HTTP_TLS_CLIENT_AUTH, rejecting modes that contradict
// the presence of a client CA.
func clientAuth(mode string, hasCA bool) (tls.ClientAuthType, error) {
	if mode == "" {
		return tls.NoClientCert, nil // netx requires and verifies when a CA is set
	}
	auth, ok := clientAuthModes[mode]
	switch {
	case !ok:
		return 0, fmt.Errorf("HTTP_TLS_CLIENT_AUTH=%q: want none, request, require, verify_if_given or require_and_verify", mode)
	case auth == tls.NoClientCert && hasCA:
		return 0, errors.New("HTTP_TLS_CLIENT_AUTH=none contradicts HTTP_TLS_CLIENT_CA_FILE")
	case (auth == tls.VerifyClientCertIfGiven || auth == tls.RequireAndVerifyClientCert) && !hasCA:
		return 0, fmt.Errorf("HTTP_TLS_CLIENT_AUTH=%s needs HTTP_TLS_CLIENT_CA_FILE", mode)
	}
	return auth, nil
}

// merge fills the fields code left unset with the environment's: code-provided
// config always wins. The exceptions only tighten: failClosed
// (HTTP_RATE_LIMIT_FAIL_CLOSED) turns on FailClosed of a code rate limiter.
func merge(code, env netx.WSConfig, failClosed bool) netx.WSConfig {
	out := code
	if out.TLS == nil {
		out.TLS = env.TLS
	}
	if len(out.TrustedProxies) == 0 {
		out.TrustedProxies = env.TrustedProxies
	}
	if len(out.AllowedOrigins) == 0 && out.CORS == nil {
		out.AllowedOrigins = env.AllowedOrigins
	}
	if env.StressControl != nil {
		sc := netx.StressControlConfig{}
		if out.StressControl != nil {
			sc = *out.StressControl
		}
		if sc.DefaultMaxConcurrent <= 0 {
			sc.DefaultMaxConcurrent = env.StressControl.DefaultMaxConcurrent
		}
		out.StressControl = &sc
	}
	if failClosed && out.RateLimiter != nil {
		rl := *out.RateLimiter
		rl.FailClosed = true
		out.RateLimiter = &rl
	}
	return out
}

// splitCSV returns the trimmed, non-empty entries of csv, nil when none.
func splitCSV(csv string) []string {
	var out []string
	for p := range strings.SplitSeq(csv, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

package httpserver

import (
	"crypto/tls"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/netx"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromEnv_Empty(t *testing.T) {
	cfg, err := ConfigFromEnv(&environment.Environment{})
	require.NoError(t, err)
	assert.Equal(t, httpx.WSConfig{}, cfg)
}

func TestConfigFromEnv_Full(t *testing.T) {
	cfg, err := ConfigFromEnv(&environment.Environment{
		HTTPTLSCertFile:     " /tls/tls.crt ",
		HTTPTLSKeyFile:      "/tls/tls.key",
		HTTPTLSClientCAFile: "/tls/ca.crt",
		HTTPTLSClientAuth:   "Verify_If_Given",
		HTTPTrustedProxies:  "private, 203.0.113.0/24,",
		HTTPMaxConcurrent:   512,
		HTTPAllowedOrigins:  "https://app.example.com, https://admin.example.com",
		AllowedOrigins:      "https://ignored.example.com",
	})
	require.NoError(t, err)
	assert.Equal(t, &netx.TLSConfig{CertFile: "/tls/tls.crt", KeyFile: "/tls/tls.key", ClientCAFile: "/tls/ca.crt", ClientAuth: tls.VerifyClientCertIfGiven}, cfg.TLS)
	assert.Equal(t, []string{"private", "203.0.113.0/24"}, cfg.TrustedProxies)
	assert.Equal(t, 512, cfg.StressControl.DefaultMaxConcurrent)
	assert.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.AllowedOrigins)
}

func TestConfigFromEnv_AllowedOriginsFallback(t *testing.T) {
	cfg, err := ConfigFromEnv(&environment.Environment{AllowedOrigins: "https://a.example.com"})
	require.NoError(t, err)
	assert.Equal(t, []string{"https://a.example.com"}, cfg.AllowedOrigins)
}

func TestConfigFromEnv_ClientAuthModes(t *testing.T) {
	for mode, want := range map[string]tls.ClientAuthType{
		"":                   tls.NoClientCert,
		"request":            tls.RequestClientCert,
		"require":            tls.RequireAnyClientCert,
		"require_and_verify": tls.RequireAndVerifyClientCert,
	} {
		cfg, err := ConfigFromEnv(&environment.Environment{HTTPTLSCertFile: "c", HTTPTLSKeyFile: "k", HTTPTLSClientCAFile: "ca", HTTPTLSClientAuth: mode})
		require.NoError(t, err, mode)
		assert.Equal(t, want, cfg.TLS.ClientAuth, mode)
	}
	cfg, err := ConfigFromEnv(&environment.Environment{HTTPTLSCertFile: "c", HTTPTLSKeyFile: "k", HTTPTLSClientAuth: "none"})
	require.NoError(t, err)
	assert.Equal(t, tls.NoClientCert, cfg.TLS.ClientAuth)
}

func TestConfigFromEnv_Errors(t *testing.T) {
	cases := map[string]environment.Environment{
		"must be set together":        {HTTPTLSCertFile: "c"},
		"need HTTP_TLS_CERT_FILE":     {HTTPTLSClientCAFile: "ca"},
		"want none, request":          {HTTPTLSCertFile: "c", HTTPTLSKeyFile: "k", HTTPTLSClientAuth: "always"},
		"contradicts":                 {HTTPTLSCertFile: "c", HTTPTLSKeyFile: "k", HTTPTLSClientCAFile: "ca", HTTPTLSClientAuth: "none"},
		"needs HTTP_TLS_CLIENT_CA":    {HTTPTLSCertFile: "c", HTTPTLSKeyFile: "k", HTTPTLSClientAuth: "require_and_verify"},
		"HTTP_MAX_CONCURRENT=-1":      {HTTPMaxConcurrent: -1},
		"trusted proxy":               {HTTPTrustedProxies: "10.0.0.1/99"},
		"cannot be combined":          {HTTPAllowedOrigins: "*"},
		"invalid CORS origin":         {HTTPAllowedOrigins: "https://*.example.com"},
		"need HTTP_TLS_CERT_FILE and": {HTTPTLSClientAuth: "request"},
	}
	for want, env := range cases {
		_, err := ConfigFromEnv(&env)
		assert.ErrorContains(t, err, want)
	}
}

// Code-provided config wins over the environment.
func TestMergePrecedence(t *testing.T) {
	env := httpx.WSConfig{
		TLS:            &netx.TLSConfig{CertFile: "env.crt"},
		TrustedProxies: []string{"private"},
		AllowedOrigins: []string{"https://env.example.com"},
		StressControl:  &httpx.StressControlConfig{DefaultMaxConcurrent: 99},
	}

	got := merge(httpx.WSConfig{}, env, false)
	assert.Equal(t, env.TLS, got.TLS)
	assert.Equal(t, env.TrustedProxies, got.TrustedProxies)
	assert.Equal(t, env.AllowedOrigins, got.AllowedOrigins)
	assert.Equal(t, 99, got.StressControl.DefaultMaxConcurrent)

	codeStress := &httpx.StressControlConfig{DefaultMaxConcurrent: 7, BufferBodyBytes: 1}
	code := httpx.WSConfig{
		TLS:            &netx.TLSConfig{CertFile: "code.crt"},
		TrustedProxies: []string{"10.0.0.0/8"},
		AllowedOrigins: []string{"https://code.example.com"},
		StressControl:  codeStress,
	}
	got = merge(code, env, false)
	assert.Equal(t, "code.crt", got.TLS.CertFile)
	assert.Equal(t, []string{"10.0.0.0/8"}, got.TrustedProxies)
	assert.Equal(t, []string{"https://code.example.com"}, got.AllowedOrigins)
	assert.Equal(t, 7, got.StressControl.DefaultMaxConcurrent)

	// A code StressControl without a limit takes the env limit, not mutated.
	codeStress.DefaultMaxConcurrent = 0
	got = merge(code, env, false)
	assert.Equal(t, 99, got.StressControl.DefaultMaxConcurrent)
	assert.Equal(t, int64(1), got.StressControl.BufferBodyBytes)
	assert.Zero(t, codeStress.DefaultMaxConcurrent)

	// A code CORS policy is not mixed with env origins.
	got = merge(httpx.WSConfig{CORS: &httpx.CorsConfig{}}, env, false)
	assert.Empty(t, got.AllowedOrigins)
}

func TestMergeRateLimitFailClosed(t *testing.T) {
	assert.Nil(t, merge(httpx.WSConfig{}, httpx.WSConfig{}, true).RateLimiter, "no limiter without code")

	rl := &httpx.RedisRateLimiterConfig{KeyPrefix: "x"}
	got := merge(httpx.WSConfig{RateLimiter: rl}, httpx.WSConfig{}, true)
	assert.True(t, got.RateLimiter.FailClosed)
	assert.Equal(t, "x", got.RateLimiter.KeyPrefix)
	assert.False(t, rl.FailClosed, "the caller's config is not mutated")

	assert.Same(t, rl, merge(httpx.WSConfig{RateLimiter: rl}, httpx.WSConfig{}, false).RateLimiter)
}

func TestComponentConfig(t *testing.T) {
	c := New(":8080", &httpx.WSConfig{AllowedOrigins: []string{"https://code.example.com"}})
	cfg, err := c.config(&environment.Environment{
		HTTPAllowedOrigins: "https://env.example.com",
		HTTPTrustedProxies: "private",
		HTTPMaxConcurrent:  64,
	})
	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.ServerPort)
	assert.Equal(t, []string{"https://code.example.com"}, cfg.AllowedOrigins)
	assert.Equal(t, []string{"private"}, cfg.TrustedProxies)
	assert.Equal(t, 64, cfg.StressControl.DefaultMaxConcurrent)
}

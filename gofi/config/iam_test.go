package config

import (
	"errors"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
)

func TestIAM_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("JWT_SECRET", "a-very-long-test-secret-key-32+chars")
	t.Setenv("JWT_ISSUER", "billing")
	t.Setenv("JWT_AUDIENCE", "billing-api")
	t.Setenv("ACCESS_TOKEN_TTL", "10m")
	t.Setenv("REFRESH_TOKEN_TTL", "48h")
	t.Setenv("CACHE_TYPE", "redis")
	t.Setenv("CACHE_URI", "localhost:6379")
	t.Setenv("CACHE_PASSWORD", "pw")
	t.Setenv("CACHE_USE_TLS", "true")
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "gid")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "gsecret")
	t.Setenv("OAUTH_GOOGLE_REDIRECT_URI", "https://app/callback")
	t.Setenv("IAM_LOGIN_MAX_ATTEMPTS", "8")
	t.Setenv("IAM_LOGIN_LOCKOUT", "30m")

	cfg := IAM(environment.Instance())

	if cfg.LoginMaxAttempts != 8 || cfg.LoginLockout != 30*time.Minute {
		t.Errorf("login throttling not mapped: %d / %v", cfg.LoginMaxAttempts, cfg.LoginLockout)
	}

	if cfg.JWTSecret != "a-very-long-test-secret-key-32+chars" {
		t.Errorf("JWTSecret not mapped: %q", cfg.JWTSecret)
	}
	if cfg.RedisAddr != "localhost:6379" || cfg.RedisPassword != "pw" || !cfg.RedisTLS {
		t.Errorf("redis session store not mapped: %+v", cfg)
	}
	if cfg.Security == nil || cfg.Security.AccessTokenTTL != 10*time.Minute ||
		cfg.Security.RefreshTokenTTL != 48*time.Hour || cfg.Security.Issuer != "billing" ||
		cfg.Security.Audience != "billing-api" {
		t.Errorf("security not mapped: %+v", cfg.Security)
	}
	if len(cfg.IDPs) != 1 || cfg.IDPs[0].Provider != "google" ||
		cfg.IDPs[0].ClientID != "gid" || cfg.IDPs[0].RedirectURI != "https://app/callback" {
		t.Errorf("google IDP not mapped: %+v", cfg.IDPs)
	}
}

// The in-memory fallback used to be silent; it is refused in stage and prod.
func TestIAMInMemory(t *testing.T) {
	cases := []struct {
		env, cache string
		inMemory   bool
		err        error
	}{
		{"prod", "redis", false, nil},
		{"prod", "", true, ErrIAMInMemorySessions},
		{"stage", "oci", true, ErrIAMInMemorySessions},
		{"dev", "", true, nil},
		{"test", "", true, nil},
		{"", "", true, nil},
	}
	for _, c := range cases {
		inMem, err := IAMInMemory(&environment.Environment{AppEnvironment: c.env, CacheType: c.cache})
		if inMem != c.inMemory || !errors.Is(err, c.err) {
			t.Errorf("env=%q cache=%q: got %v %v, want %v %v", c.env, c.cache, inMem, err, c.inMemory, c.err)
		}
	}
}

func TestIAM_NoRedisWhenCacheNotRedis(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("CACHE_TYPE", "")
	t.Setenv("CACHE_URI", "localhost:6379")

	cfg := IAM(environment.Instance())
	if cfg.RedisAddr != "" {
		t.Errorf("expected empty RedisAddr (in-memory) when cache is not redis, got %q", cfg.RedisAddr)
	}
	if len(cfg.IDPs) != 0 {
		t.Errorf("expected no IDPs without OAuth creds, got %+v", cfg.IDPs)
	}
}

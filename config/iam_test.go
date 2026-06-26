package config

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
)

func TestIAM_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("JWT_SECRET", "a-very-long-test-secret-key-32+chars")
	t.Setenv("JWT_ISSUER", "billing")
	t.Setenv("ACCESS_TOKEN_TTL", "10m")
	t.Setenv("REFRESH_TOKEN_TTL", "48h")
	t.Setenv("CACHE_TYPE", "redis")
	t.Setenv("CACHE_URI", "localhost:6379")
	t.Setenv("CACHE_PASSWORD", "pw")
	t.Setenv("CACHE_USE_TLS", "true")
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "gid")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "gsecret")
	t.Setenv("OAUTH_GOOGLE_REDIRECT_URI", "https://app/callback")

	cfg := IAM(environment.Instance())

	if cfg.JWTSecret != "a-very-long-test-secret-key-32+chars" {
		t.Errorf("JWTSecret not mapped: %q", cfg.JWTSecret)
	}
	if cfg.RedisAddr != "localhost:6379" || cfg.RedisPassword != "pw" || !cfg.RedisTLS {
		t.Errorf("redis session store not mapped: %+v", cfg)
	}
	if cfg.Security == nil || cfg.Security.AccessTokenTTL != 10*time.Minute ||
		cfg.Security.RefreshTokenTTL != 48*time.Hour || cfg.Security.Issuer != "billing" {
		t.Errorf("security not mapped: %+v", cfg.Security)
	}
	if len(cfg.IDPs) != 1 || cfg.IDPs[0].Provider != "google" ||
		cfg.IDPs[0].ClientID != "gid" || cfg.IDPs[0].RedirectURI != "https://app/callback" {
		t.Errorf("google IDP not mapped: %+v", cfg.IDPs)
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

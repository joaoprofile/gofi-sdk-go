package config

import (
	"github.com/joaoprofile/gofi/base/environment"
	iamconfig "github.com/joaoprofile/gofi/iam/config"
)

// IAM builds iam's DefaultConfig from the environment, bridging gofi's env
// schema to the IAM library without the IAM module ever importing
// base/environment. Mapping:
//
//   - JWT_SECRET                     → JWTSecret
//   - CACHE_* (when CACHE_TYPE=redis) → Redis session store
//   - OAUTH_GOOGLE_*                 → Google IDP
//   - ACCESS_TOKEN_TTL / REFRESH_TOKEN_TTL / JWT_ISSUER → SecurityConfig
//
// Remaining defaults are applied by iam (SecurityConfig.ApplyDefaults).
func IAM(env *environment.Environment) iamconfig.DefaultConfig {
	cfg := iamconfig.DefaultConfig{
		JWTSecret: env.JWTSecret,
		Security: &iamconfig.SecurityConfig{
			AccessTokenTTL:  env.AccessTokenTTL,
			RefreshTokenTTL: env.RefreshTokenTTL,
			Issuer:          env.JWTIssuer,
		},
	}

	// Use the Redis session store only when the cache backend is Redis.
	if env.GetCacheType() == environment.REDIS_CACHE {
		cfg.RedisAddr = env.CacheURI
		cfg.RedisPassword = env.CachePassword
		cfg.RedisTLS = env.CacheUseTLS
	}

	// Register the Google IDP when its OAuth credentials are present.
	if env.OAuthGoogleClientID != "" {
		cfg.IDPs = append(cfg.IDPs, iamconfig.IDPConfig{
			Provider:     "google",
			ClientID:     env.OAuthGoogleClientID,
			ClientSecret: env.OAuthGoogleClientSecret,
			RedirectURI:  env.OAuthGoogleRedirectURI,
		})
	}

	return cfg
}

package config

import (
	"errors"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	iamconfig "github.com/joaoprofile/gofi-sdk-go/iam/config"
)

// IAM builds iam's DefaultConfig from the environment, bridging gofi's env
// schema to the IAM library without the IAM module ever importing
// base/environment. Mapping:
//
//   - JWT_SECRET                     → JWTSecret
//   - JWT_KEY_ID / JWT_PREVIOUS_KEY_ID / JWT_PREVIOUS_SECRET → key rotation
//   - CACHE_* (when CACHE_TYPE=redis) → Redis session store
//   - OAUTH_GOOGLE_*                 → Google IDP
//   - ACCESS_TOKEN_TTL / REFRESH_TOKEN_TTL / JWT_ISSUER / JWT_AUDIENCE → SecurityConfig
//   - IAM_LOGIN_MAX_ATTEMPTS / IAM_LOGIN_LOCKOUT → login throttling (Redis
//     with CACHE_TYPE=redis, like the session store and single-use tickets)
//
// Remaining defaults are applied by iam (SecurityConfig.ApplyDefaults), which
// also verifies the issuer; set JWT_AUDIENCE so each service accepts only
// tokens issued for it.
func IAM(env *environment.Environment) iamconfig.DefaultConfig {
	cfg := iamconfig.DefaultConfig{
		JWTSecret:         env.JWTSecret,
		JWTKeyID:          env.JWTKeyID,
		JWTPreviousKeyID:  env.JWTPreviousKeyID,
		JWTPreviousSecret: env.JWTPreviousSecret,
		LoginMaxAttempts:  env.IAMLoginMaxAttempts,
		LoginLockout:      env.IAMLoginLockout,
		Security: &iamconfig.SecurityConfig{
			AccessTokenTTL:  env.AccessTokenTTL,
			RefreshTokenTTL: env.RefreshTokenTTL,
			Issuer:          env.JWTIssuer,
			Audience:        env.JWTAudience,
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

// ErrIAMInMemorySessions refuses in-memory iam state outside dev and test.
var ErrIAMInMemorySessions = errors.New("iam: CACHE_TYPE is not redis, so sessions, login throttling and " +
	"tenant tickets would live in one process's memory (lost on restart, not shared by replicas); set CACHE_TYPE=redis")

// IAMInMemory reports whether iam falls back to in-memory state (CACHE_TYPE
// is not redis). The fallback is refused with ErrIAMInMemorySessions in
// stage and prod; in dev and test the caller should warn.
func IAMInMemory(env *environment.Environment) (bool, error) {
	if env.GetCacheType() == environment.REDIS_CACHE {
		return false, nil
	}
	switch env.GetEnvironmentType() {
	case environment.ENV_PROD, environment.ENV_STAGE:
		return true, ErrIAMInMemorySessions
	}
	return true, nil
}

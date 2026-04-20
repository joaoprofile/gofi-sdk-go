package config

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyDefaults_SetsAllDefaultValues(t *testing.T) {
	s := &SecurityConfig{}
	s.ApplyDefaults()

	assert.Equal(t, 15*time.Minute, s.AccessTokenTTL)
	assert.Equal(t, 7*24*time.Hour, s.RefreshTokenTTL)
	assert.Equal(t, "gofi/iam", s.Issuer)
	assert.Equal(t, "iam_rt", s.CookieName)
	assert.Equal(t, "/auth/refresh", s.CookiePath)
	assert.Equal(t, http.SameSiteStrictMode, s.CookieSameSite)
	assert.Equal(t, "iam_idp_state", s.IDPStateCookieName)
	assert.Equal(t, 10*time.Minute, s.IDPStateTTL)
}

func TestApplyDefaults_DoesNotOverwriteExistingValues(t *testing.T) {
	s := &SecurityConfig{
		AccessTokenTTL:     30 * time.Minute,
		RefreshTokenTTL:    14 * 24 * time.Hour,
		Issuer:             "my-app",
		CookieName:         "my_cookie",
		CookiePath:         "/refresh",
		CookieSameSite:     http.SameSiteLaxMode,
		IDPStateCookieName: "my_state",
		IDPStateTTL:        5 * time.Minute,
	}
	s.ApplyDefaults()

	assert.Equal(t, 30*time.Minute, s.AccessTokenTTL)
	assert.Equal(t, 14*24*time.Hour, s.RefreshTokenTTL)
	assert.Equal(t, "my-app", s.Issuer)
	assert.Equal(t, "my_cookie", s.CookieName)
	assert.Equal(t, "/refresh", s.CookiePath)
	assert.Equal(t, http.SameSiteLaxMode, s.CookieSameSite)
	assert.Equal(t, "my_state", s.IDPStateCookieName)
	assert.Equal(t, 5*time.Minute, s.IDPStateTTL)
}

func TestApplyDefaults_IsIdempotent(t *testing.T) {
	s := &SecurityConfig{}
	s.ApplyDefaults()
	first := *s
	s.ApplyDefaults()

	assert.Equal(t, first, *s)
}

func TestApplyDefaults_PartialValues(t *testing.T) {
	s := &SecurityConfig{
		Issuer:     "custom-issuer",
		CookieName: "custom_cookie",
	}
	s.ApplyDefaults()

	assert.Equal(t, "custom-issuer", s.Issuer)
	assert.Equal(t, "custom_cookie", s.CookieName)
	assert.Equal(t, 15*time.Minute, s.AccessTokenTTL)
	assert.Equal(t, 7*24*time.Hour, s.RefreshTokenTTL)
}

func TestConfig_ZeroValues(t *testing.T) {
	cfg := Config{}
	assert.Nil(t, cfg.Auth)
	assert.Nil(t, cfg.User)
	assert.Nil(t, cfg.Tenant)
	assert.Nil(t, cfg.Token)
	assert.Nil(t, cfg.Session)
	assert.Nil(t, cfg.RBAC)
	assert.Nil(t, cfg.IDPs)
	assert.Nil(t, cfg.OnEvent)
}

func TestDefaultConfig_ZeroValues(t *testing.T) {
	cfg := DefaultConfig{}
	assert.Empty(t, cfg.JWTSecret)
	assert.Empty(t, cfg.RedisAddr)
	assert.Nil(t, cfg.IDPs)
	assert.Nil(t, cfg.Security)
	assert.Nil(t, cfg.OnEvent)
}

func TestIDPConfig_Fields(t *testing.T) {
	cfg := IDPConfig{
		Provider:     "google",
		ClientID:     "id",
		ClientSecret: "secret",
		RedirectURI:  "https://example.com/callback",
		IssuerURL:    "https://accounts.google.com",
		Scopes:       []string{"profile"},
	}
	assert.Equal(t, "google", cfg.Provider)
	assert.Equal(t, "id", cfg.ClientID)
	assert.Equal(t, []string{"profile"}, cfg.Scopes)
}

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/iam/core"
	"github.com/gofi-labs/gofi-sdk-go/iam/port"
	"github.com/gofi-labs/gofi-sdk-go/iam/provider/memory"
	"github.com/gofi-labs/gofi-sdk-go/iam/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRBACPort is a test double for port.RBACPort.
type stubRBACPort struct{}

func (s *stubRBACPort) Enforce(_ types.Claims, _, _ string) bool     { return true }
func (s *stubRBACPort) Permissions(_ types.Claims) []port.Permission { return nil }

// minimalConfig returns a Config that satisfies all validation constraints.
func minimalConfig() Config {
	sec := SecurityConfig{}
	sec.ApplyDefaults()
	return Config{
		Session:  memory.NewTestProvider(),
		RBAC:     &stubRBACPort{},
		Security: sec,
	}
}

func TestNew_MissingSession_ReturnsError(t *testing.T) {
	sec := SecurityConfig{}
	sec.ApplyDefaults()
	cfg := Config{
		RBAC:     &stubRBACPort{},
		Security: sec,
	}
	_, err := New(cfg)
	assert.ErrorIs(t, err, core.ErrSessionPortRequired)
}

func TestNew_AccessTokenTTLExceeded_ReturnsError(t *testing.T) {
	sec := SecurityConfig{
		AccessTokenTTL:  61 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
	}
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: sec,
	}
	_, err := New(cfg)
	assert.ErrorIs(t, err, core.ErrAccessTokenTTLExceeded)
}

func TestNew_RefreshTokenTTLExceeded_ReturnsError(t *testing.T) {
	sec := SecurityConfig{
		RefreshTokenTTL: 91 * 24 * time.Hour,
	}
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: sec,
	}
	_, err := New(cfg)
	assert.ErrorIs(t, err, core.ErrRefreshTokenTTLExceeded)
}

func TestNew_WithCustomAuth_Succeeds(t *testing.T) {
	sec := SecurityConfig{}
	sec.ApplyDefaults()

	customAuth := &stubCustomAuth{}
	cfg := Config{
		Auth:     customAuth,
		Session:  memory.NewTestProvider(),
		Security: sec,
	}
	svc, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNew_WithNilOnEvent_Succeeds(t *testing.T) {
	cfg := minimalConfig()
	cfg.OnEvent = nil
	svc, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNew_AppliesSecurityDefaults(t *testing.T) {
	sec := SecurityConfig{} // zero values — defaults should be applied
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: sec,
	}
	svc, err := New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_MemoryProvider_Succeeds(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_InvalidJWTSecret_ReturnsError(t *testing.T) {
	_, err := NewDefault(DefaultConfig{
		JWTSecret: "short",
	})
	assert.Error(t, err)
	assert.ErrorIs(t, err, core.ErrJWTSecretTooShort)
}

func TestNewDefault_NilSecurity_Succeeds(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		Security:  nil,
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_WithOnEvent_Succeeds(t *testing.T) {
	var received []types.IAMEvent
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		OnEvent: func(_ context.Context, e types.IAMEvent) {
			received = append(received, e)
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_WithGoogleIDP_Succeeds(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		IDPs: []IDPConfig{
			{Provider: "google", ClientID: "cid", ClientSecret: "secret", RedirectURI: "https://app.com/cb"},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_WithMicrosoftIDP_Succeeds(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		IDPs: []IDPConfig{
			{Provider: "microsoft", ClientID: "cid", ClientSecret: "secret", RedirectURI: "https://app.com/cb"},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_WithOIDCIDP_Succeeds(t *testing.T) {
	svc, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		IDPs: []IDPConfig{
			{
				Provider:     "oidc",
				ClientID:     "cid",
				ClientSecret: "secret",
				RedirectURI:  "https://app.com/cb",
				IssuerURL:    "https://accounts.example.com",
			},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewDefault_OIDCWithoutIssuerURL_ReturnsError(t *testing.T) {
	_, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		IDPs: []IDPConfig{
			{Provider: "oidc", ClientID: "cid", ClientSecret: "secret"},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IssuerURL is required")
}

func TestNewDefault_UnknownIDPProvider_ReturnsError(t *testing.T) {
	_, err := NewDefault(DefaultConfig{
		JWTSecret: "a-32-byte-secret-key-for-testing!",
		IDPs: []IDPConfig{
			{Provider: "okta", ClientID: "cid", ClientSecret: "secret"},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown IDP provider")
}

func TestBuildIDPProvider_Google(t *testing.T) {
	p, err := buildIDPProvider(IDPConfig{
		Provider:     "google",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "https://app.com/cb",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "google", p.ProviderName())
}

func TestBuildIDPProvider_Microsoft(t *testing.T) {
	p, err := buildIDPProvider(IDPConfig{
		Provider:     "microsoft",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "https://app.com/cb",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "microsoft", p.ProviderName())
}

func TestBuildIDPProvider_OIDC(t *testing.T) {
	p, err := buildIDPProvider(IDPConfig{
		Provider:     "oidc",
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "https://app.com/cb",
		IssuerURL:    "https://accounts.example.com",
	})
	require.NoError(t, err)
	assert.NotNil(t, p)
	assert.Equal(t, "oidc", p.ProviderName())
}

func TestBuildIDPProvider_OIDCMissingIssuerURL_ReturnsError(t *testing.T) {
	_, err := buildIDPProvider(IDPConfig{
		Provider:  "oidc",
		ClientID:  "cid",
		IssuerURL: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IssuerURL is required")
}

func TestBuildIDPProvider_Unknown_ReturnsError(t *testing.T) {
	_, err := buildIDPProvider(IDPConfig{Provider: "saml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown IDP provider")
}

func TestValidate_ValidConfig(t *testing.T) {
	sec := SecurityConfig{}
	sec.ApplyDefaults()
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: sec,
	}
	assert.NoError(t, validate(cfg))
}

func TestValidate_NilSession(t *testing.T) {
	cfg := Config{}
	err := validate(cfg)
	assert.ErrorIs(t, err, core.ErrSessionPortRequired)
}

func TestValidate_AccessTokenTTLAtLimit(t *testing.T) {
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: SecurityConfig{AccessTokenTTL: 60 * time.Minute},
	}
	assert.NoError(t, validate(cfg))
}

func TestValidate_AccessTokenTTLOverLimit(t *testing.T) {
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: SecurityConfig{AccessTokenTTL: 61 * time.Minute},
	}
	assert.ErrorIs(t, validate(cfg), core.ErrAccessTokenTTLExceeded)
}

func TestValidate_RefreshTokenTTLAtLimit(t *testing.T) {
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: SecurityConfig{RefreshTokenTTL: 90 * 24 * time.Hour},
	}
	assert.NoError(t, validate(cfg))
}

func TestValidate_RefreshTokenTTLOverLimit(t *testing.T) {
	cfg := Config{
		Session:  memory.NewTestProvider(),
		Security: SecurityConfig{RefreshTokenTTL: 91 * 24 * time.Hour},
	}
	assert.ErrorIs(t, validate(cfg), core.ErrRefreshTokenTTLExceeded)
}

// stubCustomAuth is a minimal custom AuthPort for testing.
type stubCustomAuth struct{}

func (s *stubCustomAuth) Authenticate(_ context.Context, _ port.AuthInput) (*port.AuthResult, error) {
	return nil, nil
}
func (s *stubCustomAuth) SelectTenant(_ context.Context, _ port.SelectTenantInput) (*types.Session, error) {
	return nil, nil
}
func (s *stubCustomAuth) Logout(_ context.Context, _ string) error { return nil }
func (s *stubCustomAuth) RefreshToken(_ context.Context, _ string) (*types.Session, error) {
	return nil, nil
}
func (s *stubCustomAuth) ValidateToken(_ context.Context, _ string) (*types.Claims, error) {
	return nil, nil
}

func TestValidate_SessionMaxLifetimeOverLimit(t *testing.T) {
	cfg := Config{Session: memory.NewTestProvider(), Security: SecurityConfig{SessionMaxLifetime: 91 * 24 * time.Hour}}
	assert.ErrorIs(t, validate(cfg), core.ErrSessionMaxLifetimeExceeded)
	cfg.Security.SessionMaxLifetime = 90 * 24 * time.Hour
	assert.NoError(t, validate(cfg))
}

func TestValidate_TicketSecretRequiredWhenLoginEnabled(t *testing.T) {
	d := &directoryStub{user: &types.User{ID: "u1"}}
	cases := map[string]Config{
		"built-in login": {Session: memory.NewTestProvider(), User: d, Tenant: d},
		"idp login":      {Session: memory.NewTestProvider(), Auth: &stubCustomAuth{}, IDPs: map[string]port.IDPAuthPort{"x": nil}},
	}
	for name, cfg := range cases {
		assert.ErrorIs(t, validate(cfg), core.ErrTenantTicketSecretRequired, name)

		cfg.Security.TenantTicketSecret = []byte("short")
		assert.ErrorIs(t, validate(cfg), core.ErrTenantTicketSecretTooShort, name)

		cfg.Security.TenantTicketSecret = []byte("0123456789abcdef0123456789abcdef")
		assert.NoError(t, validate(cfg), name)

		cfg.Security.TenantTicketSecret = nil
		cfg.Security.InsecureSkipTenantTicket = true
		assert.NoError(t, validate(cfg), name)
	}
	// Resource server (no login) and custom AuthPort do not need the secret.
	assert.NoError(t, validate(Config{Session: memory.NewTestProvider()}))
	assert.NoError(t, validate(Config{Session: memory.NewTestProvider(), Auth: &stubCustomAuth{}, User: d, Tenant: d}))
}

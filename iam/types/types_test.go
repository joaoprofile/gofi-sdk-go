package types

import (
	"testing"
	"time"
)

// ---- Claims ----

func TestClaims_Fields(t *testing.T) {
	now := time.Now()
	c := Claims{
		UserID:       "u1",
		TenantID:     "t1",
		Module:       "m1",
		Roles:        []string{"admin", "viewer"},
		SessionID:    "s1",
		AuthProvider: "google",
		Issuer:       "https://example.com",
		IssuedAt:     now,
		ExpiresAt:    now.Add(time.Hour),
	}
	if c.UserID != "u1" {
		t.Errorf("UserID=%q, want u1", c.UserID)
	}
	if len(c.Roles) != 2 {
		t.Errorf("len(Roles)=%d, want 2", len(c.Roles))
	}
}

// ---- Session ----

func TestSession_SanitizePattern(t *testing.T) {
	s := Session{
		ID:                   "sid1",
		UserID:               "u1",
		TenantID:             "t1",
		Module:               "mod",
		AccessToken:          "at",
		RefreshToken:         "raw-secret",
		RefreshTokenHash:     "hash",
		RefreshTokenLastFour: "cret",
		AuthProvider:         "local",
		ExpiresAt:            time.Now().Add(time.Hour),
		CreatedAt:            time.Now(),
		LastUsedAt:           time.Now(),
		Revoked:              false,
		IPAddress:            "127.0.0.1",
		UserAgent:            "test",
		DeviceID:             "dev1",
	}
	if s.RefreshToken != "raw-secret" {
		t.Errorf("RefreshToken=%q, want raw-secret", s.RefreshToken)
	}
	if s.RefreshTokenHash != "hash" {
		t.Errorf("RefreshTokenHash=%q, want hash", s.RefreshTokenHash)
	}
	if s.Revoked {
		t.Error("expected Revoked=false")
	}
}

func TestSession_Revocation(t *testing.T) {
	now := time.Now()
	s := Session{
		Revoked:   true,
		RevokedAt: &now,
		RevokedBy: "admin",
	}
	if !s.Revoked {
		t.Error("expected Revoked=true")
	}
	if s.RevokedBy != "admin" {
		t.Errorf("RevokedBy=%q, want admin", s.RevokedBy)
	}
}

// ---- IDPUser ----

func TestIDPUser_Fields(t *testing.T) {
	u := IDPUser{
		ExternalID:    "ext1",
		Provider:      "google",
		Email:         "user@example.com",
		EmailVerified: true,
		Name:          "Test User",
		PictureURL:    "https://example.com/pic.jpg",
		RawClaims:     map[string]any{"sub": "ext1"},
	}
	if u.Provider != "google" {
		t.Errorf("Provider=%q, want google", u.Provider)
	}
	if !u.EmailVerified {
		t.Error("expected EmailVerified=true")
	}
	if u.RawClaims["sub"] != "ext1" {
		t.Errorf("RawClaims[sub]=%v, want ext1", u.RawClaims["sub"])
	}
}

// ---- Tenant / TenantAccess ----

func TestTenant_Fields(t *testing.T) {
	tenant := Tenant{
		ID:      "tenant1",
		Name:    "Acme Corp",
		Modules: []string{"crm", "billing"},
		Active:  true,
	}
	if !tenant.Active {
		t.Error("expected Active=true")
	}
	if len(tenant.Modules) != 2 {
		t.Errorf("len(Modules)=%d, want 2", len(tenant.Modules))
	}
}

func TestTenantAccess_Fields(t *testing.T) {
	ta := TenantAccess{
		Tenant:  Tenant{ID: "t1", Name: "Acme"},
		Modules: []string{"crm"},
		Roles:   []string{"admin"},
	}
	if ta.Tenant.ID != "t1" {
		t.Errorf("Tenant.ID=%q, want t1", ta.Tenant.ID)
	}
}

// ---- User / ExternalIdentity ----

func TestUser_Fields(t *testing.T) {
	u := User{
		ID:            "u1",
		Email:         "user@example.com",
		PasswordHash:  "$2a$10$...",
		Active:        true,
		EmailVerified: true,
		ExternalIdentities: []ExternalIdentity{
			{
				Provider:   "google",
				ExternalID: "ext1",
				Email:      "user@gmail.com",
				LinkedAt:   time.Now(),
			},
		},
	}
	if !u.Active {
		t.Error("expected Active=true")
	}
	if len(u.ExternalIdentities) != 1 {
		t.Errorf("len(ExternalIdentities)=%d, want 1", len(u.ExternalIdentities))
	}
	if u.ExternalIdentities[0].Provider != "google" {
		t.Errorf("Provider=%q, want google", u.ExternalIdentities[0].Provider)
	}
}

// ---- IAMEvent / EventType constants ----

func TestEventTypeConstants(t *testing.T) {
	events := []EventType{
		EventLogin,
		EventLoginFailed,
		EventIDPLogin,
		EventIDPLoginFailed,
		EventNewUser,
		EventTenantSelected,
		EventLogout,
		EventLogoutAll,
		EventTokenRefreshed,
		EventTokenRefreshFailed,
		EventAccessDenied,
		EventTenantAccessDenied,
		EventTokenValidated,
		EventTokenInvalid,
		EventSessionRevoked,
		EventSuspiciousActivity,
	}
	for _, e := range events {
		if e == "" {
			t.Error("unexpected empty EventType constant")
		}
	}
}

func TestIAMEvent_Fields(t *testing.T) {
	now := time.Now()
	ev := IAMEvent{
		Type:      EventLogin,
		UserID:    "u1",
		TenantID:  "t1",
		Module:    "mod",
		SessionID: "s1",
		Provider:  "local",
		IPAddress: "127.0.0.1",
		UserAgent: "test-agent",
		DeviceID:  "dev1",
		Timestamp: now,
		Error:     nil,
		Extra:     map[string]any{"key": "value"},
	}
	if ev.Type != EventLogin {
		t.Errorf("Type=%q, want %q", ev.Type, EventLogin)
	}
	if ev.Extra["key"] != "value" {
		t.Errorf("Extra[key]=%v, want value", ev.Extra["key"])
	}
}

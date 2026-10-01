package iam

import "testing"

func TestWithTicketSecret(t *testing.T) {
	sec := SecurityConfig{}
	got := withTicketSecret(sec, "a-32-byte-secret-key-for-testing!")
	if len(got.TenantTicketSecret) < 32 {
		t.Fatalf("derived key too short: %d", len(got.TenantTicketSecret))
	}
	if sec.TenantTicketSecret != nil {
		t.Fatal("caller config must not be mutated")
	}
	explicit := withTicketSecret(SecurityConfig{TenantTicketSecret: []byte("explicit")}, "x")
	if string(explicit.TenantTicketSecret) != "explicit" {
		t.Fatal("explicit secret must be kept")
	}
}

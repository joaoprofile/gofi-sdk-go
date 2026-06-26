package mail

import (
	"errors"
	"testing"
)

func TestConfigValidate_Defaults(t *testing.T) {
	c := Config{Host: "smtp.x.com", From: Address{Email: "f@x.com"}, Username: "u"}
	if err := c.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != defaultPort || c.Timeout != defaultTimeout {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if c.Encryption != EncryptionSTARTTLS {
		t.Fatalf("default encryption should be STARTTLS, got %q", c.Encryption)
	}
	if c.Auth != AuthPlain { // username set → PLAIN
		t.Fatalf("default auth with username should be PLAIN, got %q", c.Auth)
	}
}

func TestConfigValidate_NoUsernameDefaultsToNoAuth(t *testing.T) {
	c := Config{Host: "smtp.x.com", From: Address{Email: "f@x.com"}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if c.Auth != AuthNone {
		t.Fatalf("no username → AuthNone, got %q", c.Auth)
	}
}

func TestConfigValidate_Errors(t *testing.T) {
	cases := []Config{
		{From: Address{Email: "f@x.com"}}, // no host
		{Host: "smtp.x.com"},              // no from
		{Host: "h", From: Address{Email: "f@x.com"}, Encryption: "weird"}, // bad encryption
		{Host: "h", From: Address{Email: "f@x.com"}, Auth: "weird"},       // bad auth
	}
	for i, c := range cases {
		if err := c.validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: expected ErrInvalidConfig, got %v", i, err)
		}
	}
}

func TestTLSConfig(t *testing.T) {
	c := Config{Host: "mail.x.com"}
	if c.tlsConfig().ServerName != "mail.x.com" {
		t.Fatal("default tlsConfig should set ServerName to host")
	}
}

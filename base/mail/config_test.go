package mail

import (
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
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

func TestFromEnv(t *testing.T) {
	t.Setenv("MAIL_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM_EMAIL", "no-reply@example.com")
	t.Setenv("MAIL_FROM_NAME", "BlueFamly")
	t.Setenv("MAIL_PORT", "465")
	t.Setenv("MAIL_USERNAME", "user")
	t.Setenv("MAIL_PASSWORD", "secret")
	t.Setenv("MAIL_ENCRYPTION", "tls")
	t.Setenv("MAIL_AUTH", "login")
	t.Setenv("MAIL_TIMEOUT", "5s")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv error: %v", err)
	}
	if cfg.Host != "smtp.example.com" || cfg.Port != 465 || cfg.From.Email != "no-reply@example.com" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Encryption != EncryptionTLS || cfg.Auth != AuthLogin {
		t.Fatalf("encryption/auth not mapped: %+v", cfg)
	}
	if cfg.Timeout != 5*time.Second || cfg.From.Name != "BlueFamly" {
		t.Fatalf("timeout/from-name not mapped: %+v", cfg)
	}
}

func TestFromEnv_NotConfigured(t *testing.T) {
	t.Setenv("MAIL_HOST", "")
	t.Setenv("MAIL_FROM_EMAIL", "")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	if _, err := FromEnv(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestTLSConfig(t *testing.T) {
	c := Config{Host: "mail.x.com"}
	if c.tlsConfig().ServerName != "mail.x.com" {
		t.Fatal("default tlsConfig should set ServerName to host")
	}
}

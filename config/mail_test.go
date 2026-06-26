package config

import (
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/mail"
)

func TestMail(t *testing.T) {
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

	cfg, err := Mail(environment.Instance())
	if err != nil {
		t.Fatalf("Mail error: %v", err)
	}
	if cfg.Host != "smtp.example.com" || cfg.Port != 465 || cfg.From.Email != "no-reply@example.com" {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Encryption != mail.EncryptionTLS || cfg.Auth != mail.AuthLogin {
		t.Fatalf("encryption/auth not mapped: %+v", cfg)
	}
	if cfg.Timeout != 5*time.Second || cfg.From.Name != "BlueFamly" {
		t.Fatalf("timeout/from-name not mapped: %+v", cfg)
	}
}

func TestMail_NotConfigured(t *testing.T) {
	t.Setenv("MAIL_HOST", "")
	t.Setenv("MAIL_FROM_EMAIL", "")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	if _, err := Mail(environment.Instance()); !errors.Is(err, mail.ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestNewMailer(t *testing.T) {
	t.Setenv("MAIL_HOST", "smtp.example.com")
	t.Setenv("MAIL_FROM_EMAIL", "no-reply@example.com")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	m, err := NewMailer(environment.Instance())
	if err != nil || m == nil {
		t.Fatalf("NewMailer: m=%v err=%v", m, err)
	}
}

func TestNewMailer_NotConfigured(t *testing.T) {
	t.Setenv("MAIL_HOST", "")
	t.Setenv("MAIL_FROM_EMAIL", "")
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	if _, err := NewMailer(environment.Instance()); err == nil {
		t.Fatal("expected error when not configured")
	}
}

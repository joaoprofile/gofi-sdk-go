package config

import (
	"strings"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/mail"
)

// defaultMailMaxRetries is the convenience default applied to bulk sends when
// MAIL_MAX_RETRIES is unset in the environment.
const defaultMailMaxRetries = 2

// Mail builds a mail.Config from the MAIL_* variables of the given environment.
// Returns mail.ErrNotConfigured when MAIL_HOST or MAIL_FROM_EMAIL are absent.
// Remaining defaults (port, encryption, auth, timeout) are applied by mail.New.
func Mail(env *environment.Environment) (mail.Config, error) {
	if env.MailHost == "" || env.MailFromEmail == "" {
		return mail.Config{}, mail.ErrNotConfigured
	}
	cfg := mail.Config{
		Host:     env.MailHost,
		Port:     env.MailPort,
		Username: env.MailUsername,
		Password: env.MailPassword,
		From:     mail.Address{Name: env.MailFromName, Email: env.MailFromEmail},

		Encryption: mail.Encryption(strings.ToLower(strings.TrimSpace(env.MailEncryption))),
		Auth:       mail.AuthMechanism(strings.ToLower(strings.TrimSpace(env.MailAuth))),

		Timeout:    env.MailTimeout,
		MaxRetries: env.MailMaxRetries,
		PoolSize:   env.MailPoolSize,
		HELODomain: env.MailHELODomain,
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMailMaxRetries
	}
	return cfg, nil
}

// NewMailer builds a ready mail.Mailer from the environment. Returns
// mail.ErrNotConfigured when the minimum MAIL_* settings are absent.
func NewMailer(env *environment.Environment) (mail.Mailer, error) {
	cfg, err := Mail(env)
	if err != nil {
		return nil, err
	}
	return mail.New(cfg)
}

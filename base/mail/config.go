package mail

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"
)

// Encryption is the transport security used to reach the SMTP server.
type Encryption string

const (
	EncryptionNone     Encryption = "none"     // plaintext (dev only)
	EncryptionSTARTTLS Encryption = "starttls" // upgrade on 587/25
	EncryptionTLS      Encryption = "tls"      // implicit TLS on 465
)

// AuthMechanism is the SMTP authentication scheme.
type AuthMechanism string

const (
	AuthNone    AuthMechanism = "none"
	AuthPlain   AuthMechanism = "plain"
	AuthLogin   AuthMechanism = "login"
	AuthCRAMMD5 AuthMechanism = "cram-md5"
)

// Defaults applied by the package when a value is missing.
const (
	defaultPort    = 587
	defaultTimeout = 10 * time.Second
)

// Config configures the SMTP mailer. Build it explicitly and pass it to New;
// gofi's config package can populate it from MAIL_* environment variables.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     Address

	Encryption Encryption
	Auth       AuthMechanism

	Timeout    time.Duration
	MaxRetries int // additional attempts after the first on transient failures
	PoolSize   int // bulk: reconnect every PoolSize messages (0 = single connection)
	HELODomain string

	// TLSConfig overrides the default tls.Config (ServerName = Host). Optional.
	TLSConfig *tls.Config
}

// validate enforces the minimum contract and normalizes enums/defaults.
func (c *Config) validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("%w: Host is required", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.From.Email) == "" {
		return fmt.Errorf("%w: From.Email is required", ErrInvalidConfig)
	}
	if c.Port <= 0 {
		c.Port = defaultPort
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	switch c.Encryption {
	case EncryptionNone, EncryptionSTARTTLS, EncryptionTLS:
	case "":
		c.Encryption = EncryptionSTARTTLS
	default:
		return fmt.Errorf("%w: unknown Encryption %q", ErrInvalidConfig, c.Encryption)
	}
	switch c.Auth {
	case AuthNone, AuthPlain, AuthLogin, AuthCRAMMD5:
	case "":
		if c.Username != "" {
			c.Auth = AuthPlain
		} else {
			c.Auth = AuthNone
		}
	default:
		return fmt.Errorf("%w: unknown Auth %q", ErrInvalidConfig, c.Auth)
	}
	return nil
}

// tlsConfig returns the TLS settings for the connection.
func (c *Config) tlsConfig() *tls.Config {
	if c.TLSConfig != nil {
		return c.TLSConfig
	}
	return &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
}

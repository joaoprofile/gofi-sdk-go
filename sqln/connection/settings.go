package connection

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"
	"unicode"
)

// SSL modes accepted by Settings.SSLMode. They follow libpq's names; each
// driver maps them to its own options and rejects the ones it cannot honour.
const (
	SSLDisable    = "disable"     // plaintext
	SSLAllow      = "allow"       // plaintext, TLS only if the server demands it (postgres)
	SSLPrefer     = "prefer"      // TLS when the server offers it, else plaintext
	SSLRequire    = "require"     // TLS without verifying the server identity
	SSLVerifyCA   = "verify-ca"   // TLS, certificate chain verified
	SSLVerifyFull = "verify-full" // TLS, certificate chain and host name verified
)

var sslModes = []string{SSLDisable, SSLAllow, SSLPrefer, SSLRequire, SSLVerifyCA, SSLVerifyFull}

// ErrInvalidSettings is wrapped by every Settings validation failure.
var ErrInvalidSettings = errors.New("sqln: invalid connection settings")

// Settings holds the structured connection parameters that a Driver assembles
// into its driver-specific DSN. It decouples DSN construction from any single
// configuration source: gofi's config.Database fills it from the DATABASE_*
// environment variables, but callers can build it by hand just as well.
type Settings struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	// SSLMode selects transport security (see the SSL* constants). Empty
	// means verify-full, except on a loopback host or unix socket, where it
	// means disable so local development works without certificates.
	SSLMode string
	// SSLRootCert is the PEM file of the CAs that sign the server
	// certificate (e.g. the RDS bundle); empty uses the system roots.
	SSLRootCert string
	// SSLCert and SSLKey are the PEM client certificate and key for mutual TLS.
	SSLCert string
	SSLKey  string
	// StatementTimeout makes the server cancel statements that run longer
	// (postgres statement_timeout); zero keeps the server default.
	StatementTimeout time.Duration
}

// SettingsValidator is implemented by drivers with their own constraints on
// Settings (supported SSL modes, options); BuildDSN calls it.
type SettingsValidator interface {
	ValidateSettings(s Settings) error
}

// BuildDSN validates s, generically and through d when it implements
// SettingsValidator, and returns d's DSN for it.
func BuildDSN(d Driver, s Settings) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if v, ok := d.(SettingsValidator); ok {
		if err := v.ValidateSettings(s); err != nil {
			return "", err
		}
	}
	return d.DSN(s), nil
}

// EffectiveSSLMode is the SSL mode drivers apply: SSLMode, or its default
// when empty (disable on a local host, verify-full elsewhere).
func (s Settings) EffectiveSSLMode() string {
	if s.SSLMode != "" {
		return s.SSLMode
	}
	if s.IsLocal() {
		return SSLDisable
	}
	return SSLVerifyFull
}

// Insecure reports whether the connection may run in plaintext or without
// verifying the server identity: any effective mode other than verify-ca and
// verify-full, unknown modes included. Production guards should refuse it
// unless IsLocal (e.g. a sidecar proxy that owns TLS).
func (s Settings) Insecure() bool {
	m := s.EffectiveSSLMode()
	return m != SSLVerifyCA && m != SSLVerifyFull
}

// IsLocal reports whether Host is empty, a loopback name or address, or a
// unix socket directory.
func (s Settings) IsLocal() bool {
	h := strings.TrimSuffix(strings.TrimPrefix(s.Host, "["), "]")
	if h == "" || strings.HasPrefix(h, "/") || strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// CheckSSLMode fails when the effective SSL mode is not in supported.
func (s Settings) CheckSSLMode(supported ...string) error {
	if m := s.EffectiveSSLMode(); !slices.Contains(supported, m) {
		return fmt.Errorf("%w: SSLMode %q is not supported (use one of %s)", ErrInvalidSettings, m, strings.Join(supported, ", "))
	}
	return nil
}

// Validate rejects values that no driver can place in a DSN safely: control
// characters or whitespace, and URL or DSN delimiters in Host and Name.
func (s Settings) Validate() error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalidSettings}, args...)...))
	}
	if s.Port < 0 || s.Port > 65535 {
		fail("Port %d is out of range", s.Port)
	}
	validateHost(s.Host, fail)
	if hasSpaceOrControl(s.Name) || strings.ContainsAny(s.Name, `/?#&=%\'";@:`) {
		fail("Name %q is not a database name", s.Name)
	}
	if hasControl(s.User) {
		fail("User contains control characters")
	}
	if strings.ContainsRune(s.Password, 0) {
		fail("Password contains a NUL character")
	}
	if s.SSLMode != "" && !slices.Contains(sslModes, s.SSLMode) {
		fail("unknown SSLMode %q", s.SSLMode)
	}
	if hasControl(s.SSLRootCert) || hasControl(s.SSLCert) || hasControl(s.SSLKey) {
		fail("SSL file paths contain control characters")
	}
	if (s.SSLCert == "") != (s.SSLKey == "") {
		fail("SSLCert and SSLKey must be set together")
	}
	if s.StatementTimeout < 0 {
		fail("StatementTimeout is negative")
	}
	return errors.Join(errs...)
}

// validateHost reports through fail a Host that is neither a Unix socket
// directory (leading "/") nor a plain host name or address.
func validateHost(host string, fail func(format string, args ...any)) {
	if strings.HasPrefix(host, "/") {
		if hasControl(host) {
			fail("Host contains control characters")
		}
		return
	}
	if hasSpaceOrControl(host) || strings.ContainsAny(host, `/?#@\'"&=;`) {
		fail("Host %q is not a host name or address", host)
	}
}

func hasControl(v string) bool {
	return strings.ContainsFunc(v, unicode.IsControl)
}

func hasSpaceOrControl(v string) bool {
	return strings.ContainsFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

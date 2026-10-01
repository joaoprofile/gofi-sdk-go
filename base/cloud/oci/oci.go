// Package oci resolves OCI credentials for every gofi integration (object
// storage, queue, ...), so API keys and principals are configured once.
package oci

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
)

// AuthMode selects where credentials come from. OCI never auto-detects the
// principal, so the choice is explicit; empty means AuthAPIKey.
type AuthMode string

const (
	// AuthAPIKey signs requests with a user API key (TenancyID, UserID, Fingerprint, PrivateKey).
	AuthAPIKey AuthMode = "api_key"
	// AuthInstancePrincipal uses the compute instance identity (metadata service).
	AuthInstancePrincipal AuthMode = "instance_principal"
	// AuthResourcePrincipal uses resource-principal variables (Functions, Container Instances).
	AuthResourcePrincipal AuthMode = "resource_principal"
	// AuthWorkloadIdentity uses the OKE pod's service-account identity.
	AuthWorkloadIdentity AuthMode = "workload_identity"
)

// ErrInvalidConfig is returned for missing or unsupported credential settings.
var ErrInvalidConfig = errors.New("oci: invalid credentials config")

// Config holds OCI identity settings. The API-key fields are used only when
// AuthMode is AuthAPIKey (or empty).
type Config struct {
	AuthMode AuthMode
	Region   string

	TenancyID   string
	UserID      string
	Fingerprint string
	PrivateKey  string // PEM content
	Passphrase  string // optional, for encrypted keys
}

// mask hides a secret value: "[REDACTED]" when set, "" when empty.
func mask(s string) string {
	if s == "" {
		return ""
	}
	return "[REDACTED]"
}

// plain drops Config's methods so the masked copy renders with the defaults.
type plain Config

func (c Config) redacted() plain {
	c.PrivateKey = mask(c.PrivateKey)
	c.Passphrase = mask(c.Passphrase)
	return plain(c)
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

func (c Config) GoString() string {
	r := c.redacted()
	return fmt.Sprintf("oci.Config{AuthMode:%#v, Region:%#v, TenancyID:%#v, UserID:%#v, Fingerprint:%#v, PrivateKey:%#v, Passphrase:%#v}", r.AuthMode, r.Region, r.TenancyID, r.UserID, r.Fingerprint, r.PrivateKey, r.Passphrase)
}

func (c Config) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = f.Write([]byte(c.GoString()))
		return
	}
	format := "%v"
	if f.Flag('+') {
		format = "%+v"
	}
	_, _ = fmt.Fprintf(f, format, c.redacted())
}

func (c Config) LogValue() slog.Value {
	r := c.redacted()
	return slog.GroupValue(
		slog.Any("AuthMode", r.AuthMode),
		slog.Any("Region", r.Region),
		slog.Any("TenancyID", r.TenancyID),
		slog.Any("UserID", r.UserID),
		slog.Any("Fingerprint", r.Fingerprint),
		slog.Any("PrivateKey", r.PrivateKey),
		slog.Any("Passphrase", r.Passphrase),
	)
}

func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.redacted()) }

// ConfigurationProvider returns the OCI SDK provider for cfg.
func ConfigurationProvider(cfg Config) (common.ConfigurationProvider, error) {
	switch cfg.AuthMode {
	case "", AuthAPIKey:
		return apiKeyProvider(cfg)
	case AuthInstancePrincipal:
		return auth.InstancePrincipalConfigurationProvider()
	case AuthResourcePrincipal:
		return auth.ResourcePrincipalConfigurationProvider()
	case AuthWorkloadIdentity:
		return auth.OkeWorkloadIdentityConfigurationProvider()
	default:
		return nil, fmt.Errorf("%w: unsupported auth mode %q", ErrInvalidConfig, cfg.AuthMode)
	}
}

func apiKeyProvider(cfg Config) (common.ConfigurationProvider, error) {
	switch {
	case cfg.TenancyID == "":
		return nil, fmt.Errorf("%w: tenancy id is required", ErrInvalidConfig)
	case cfg.UserID == "":
		return nil, fmt.Errorf("%w: user id is required", ErrInvalidConfig)
	case cfg.Fingerprint == "":
		return nil, fmt.Errorf("%w: fingerprint is required", ErrInvalidConfig)
	case cfg.PrivateKey == "":
		return nil, fmt.Errorf("%w: private key is required", ErrInvalidConfig)
	case cfg.Region == "":
		return nil, fmt.Errorf("%w: region is required", ErrInvalidConfig)
	}
	var passphrase *string
	if cfg.Passphrase != "" {
		passphrase = &cfg.Passphrase
	}
	return common.NewRawConfigurationProvider(cfg.TenancyID, cfg.UserID, cfg.Region, cfg.Fingerprint, cfg.PrivateKey, passphrase), nil
}

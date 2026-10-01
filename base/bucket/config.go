package bucket

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/redact"
)

// Provider selects the object-storage backend a Config targets.
type Provider string

// Supported object-storage providers.
const (
	ProviderOCI Provider = "oci"
	// ProviderS3 targets Amazon S3 or any S3-compatible service (MinIO, R2, ...).
	ProviderS3 Provider = "s3"
	// ProviderMinIO is an alias of ProviderS3 kept for existing BUCKET_PROVIDER values.
	ProviderMinIO Provider = "minio"
	// ProviderFile stores objects under a local directory (Config.Endpoint).
	ProviderFile Provider = "file"
	// ProviderMem keeps objects in memory; for tests.
	ProviderMem  Provider = "mem"
	ProviderNone Provider = "none"
)

// Config describes which object-storage backend to open and its credentials.
// Build it explicitly and pass it to a factory; gofi's config package can
// populate it from BUCKET_* environment variables. Provider-specific
// credentials are nested so the struct stays extensible as new backends appear.
type Config struct {
	Provider Provider
	Name     string
	Region   string
	Endpoint string
	// PresignMaxTTL lowers the longest validity PresignGet accepts; zero
	// keeps MaxPresignTTL, and larger values are capped at it.
	PresignMaxTTL time.Duration
	// OCICredentials holds OCI Object Storage auth fields.
	OCICredentials OCICredentials
	// S3Credentials holds MinIO / S3-compatible auth fields.
	S3Credentials S3Credentials
}

// OCIAuthMode selects how the OCI backend obtains its credentials. The OCI SDK
// never auto-detects instance identity: the caller must name the principal it
// wants, so this choice is always explicit.
type OCIAuthMode string

// Supported OCI authentication modes. Empty is treated as OCIAuthAPIKey.
const (
	// OCIAuthAPIKey signs requests with a user API key whose fields
	// (TenancyID, UserID, FingerPrint, PrivateKey) are injected via env.
	OCIAuthAPIKey OCIAuthMode = "api_key"
	// OCIAuthInstancePrincipal derives credentials from the compute
	// instance's identity via the OCI metadata service. Works only inside
	// an OCI instance; requires no key material.
	OCIAuthInstancePrincipal OCIAuthMode = "instance_principal"
	// OCIAuthResourcePrincipal derives credentials from resource-principal
	// environment variables (Functions and similar resources).
	OCIAuthResourcePrincipal OCIAuthMode = "resource_principal"
	// OCIAuthWorkloadIdentity derives credentials from an OKE pod's workload
	// identity (service-account based).
	OCIAuthWorkloadIdentity OCIAuthMode = "workload_identity"
)

// OCICredentials holds auth fields exclusive to the OCI backend. AuthMode
// selects the credential source; the API-key fields are consumed only when
// AuthMode is OCIAuthAPIKey (or empty).
type OCICredentials struct {
	AuthMode    OCIAuthMode
	Namespace   string
	TenancyID   string
	UserID      string
	FingerPrint string
	PrivateKey  string `redact:"true"`
	Passphrase  string `redact:"true"`
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c OCICredentials) String() string                { return redact.Sprint(c) }
func (c OCICredentials) GoString() string              { return redact.GoSprint(c) }
func (c OCICredentials) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c OCICredentials) LogValue() slog.Value          { return redact.LogValue(c) }
func (c OCICredentials) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

// S3Credentials holds auth fields exclusive to the S3 backend. Empty keys use
// the AWS default credential chain (IRSA, EKS Pod Identity, instance profile).
type S3Credentials struct {
	AccessKey string
	SecretKey string `redact:"true"`
	UseSSL    bool   // for custom endpoints without a scheme
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c S3Credentials) String() string                { return redact.Sprint(c) }
func (c S3Credentials) GoString() string              { return redact.GoSprint(c) }
func (c S3Credentials) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c S3Credentials) LogValue() slog.Value          { return redact.LogValue(c) }
func (c S3Credentials) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

// IsConfigured reports whether a backend is explicitly selected, i.e. Provider
// is set and is not ProviderNone.
func (c Config) IsConfigured() bool {
	return c.Provider != "" && c.Provider != ProviderNone
}

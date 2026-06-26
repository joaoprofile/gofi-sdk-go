package bucket

// Provider selects the object-storage backend a Config targets.
type Provider string

// Supported object-storage providers.
const (
	ProviderOCI   Provider = "oci"
	ProviderMinIO Provider = "minio"
	ProviderNone  Provider = "none"
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
	// OCICredentials holds OCI Object Storage auth fields.
	OCICredentials OCICredentials
	// S3Credentials holds MinIO / S3-compatible auth fields.
	S3Credentials S3Credentials
}

// OCICredentials holds auth fields exclusive to the OCI backend.
type OCICredentials struct {
	Namespace   string
	TenancyID   string
	UserID      string
	FingerPrint string
	PrivateKey  string
	Passphrase  string
}

// S3Credentials holds auth fields exclusive to the MinIO / S3 backend.
type S3Credentials struct {
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// IsConfigured reports whether a backend is explicitly selected, i.e. Provider
// is set and is not ProviderNone.
func (c Config) IsConfigured() bool {
	return c.Provider != "" && c.Provider != ProviderNone
}

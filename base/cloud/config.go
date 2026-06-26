package cloud

// ProviderName identifies a cloud-provider implementation in the registry.
type ProviderName string

// Supported cloud providers.
const (
	ProviderAWS  ProviderName = "aws"
	ProviderGCP  ProviderName = "gcp"
	ProviderOCI  ProviderName = "oci"
	ProviderNone ProviderName = "none"
)

// Config holds the credentials and endpoint settings for a cloud provider.
// Build it explicitly and pass it to Init; gofi's config package can populate
// it from CLOUD_* environment variables.
type Config struct {
	Provider   ProviderName
	Host       string
	Region     string
	Secret     string
	Token      string
	DisableSSL bool
}

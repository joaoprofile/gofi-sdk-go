package environment

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi/base/common"
	"github.com/joho/godotenv"
)

var (
	ErrLoadingEnvironmentFile   = errors.New("error loading environment file")
	ErrParsingEnvironment       = errors.New("error parsing environment variables")
	ErrInvalidEnvironment       = errors.New("invalid environment")
	ErrInvalidCloudProvider     = errors.New("invalid cloud provider")
	ErrInvalidMessagingProvider = errors.New("invalid messaging provider")
	ErrInvalidCacheType         = errors.New("invalid cache type")
)

type CloudProvider string
type MessagingProvider string
type CacheType string
type EnvironmentType string

// Cloud Providers
const (
	CLOUD_AWS  CloudProvider = "aws"
	CLOUD_GCP  CloudProvider = "gcp"
	CLOUD_OCI  CloudProvider = "oci"
	CLOUD_NONE CloudProvider = "none"
)

// Environment types
const (
	ENV_DEV   EnvironmentType = "dev"
	ENV_STAGE EnvironmentType = "stage"
	ENV_TEST  EnvironmentType = "test"
	ENV_PROD  EnvironmentType = "prod"
)

// Messaging Providers
const (
	MESSAGING_RABBITMQ MessagingProvider = "rabbitmq"
	MESSAGING_KAFKA    MessagingProvider = "kafka"
	MESSAGING_SQS_SNS  MessagingProvider = "sqs_sns"
	MESSAGING_AWS      MessagingProvider = "aws"
	MESSAGING_GCP      MessagingProvider = "gcp"
)

// Cache Types
const (
	REDIS_CACHE CacheType = "redis"
	OCI_CACHE   CacheType = "oci"
)

const APP_MAX_PARALLEL_WORKERS = 1

// Environment holds all configuration loaded from environment variables.
type Environment struct {
	AppName               string `env:"APP_NAME"`
	AppEnvironment        string `env:"APP_ENVIRONMENT"`
	AppTenant             int    `env:"APP_TENANT"`
	AppMaxParallelWorkers int    `env:"APP_MAX_PARALLEL_WORKERS"`

	// Timezone is the IANA name applied to time.Local at startup. Empty defaults
	// to Brazil (America/Sao_Paulo).
	Timezone string `env:"TIMEZONE"`

	ServiceDebug     bool   `env:"SERVICE_DEBUG"`
	ServiceDebugAddr string `env:"SERVICE_DEBUG_ADDR"`
	ServiceDebugUser string `env:"SERVICE_DEBUG_USER"`
	ServiceDebugPass string `env:"SERVICE_DEBUG_PASS"`
	ServicePort      int    `env:"PORT"`

	LogLevel  string `env:"LOG_LEVEL"`
	LogOutput string `env:"LOG_OUTPUT"`

	DatabaseDriver       string        `env:"DATABASE_DRIVER"`
	DatabaseHost         string        `env:"DATABASE_HOST"`
	DatabasePort         int           `env:"DATABASE_PORT"`
	DatabaseUser         string        `env:"DATABASE_USER"`
	DatabasePassword     string        `env:"DATABASE_PASSWORD"`
	DatabaseName         string        `env:"DATABASE_NAME"`
	DatabaseSSLMode      string        `env:"DATABASE_SSL_MODE"`
	DatabaseMigration    bool          `env:"DATABASE_MIGRATION"`
	DatabaseMaxOpenConns int           `env:"DATABASE_MAX_OPEN_CONNS"`
	DatabaseMaxIdleConns int           `env:"DATABASE_MAX_IDLE_CONNS"`
	DatabaseMaxLifetime  time.Duration `env:"DATABASE_MAX_LIFETIME"`

	CacheType     string `env:"CACHE_TYPE"`
	CacheURI      string `env:"CACHE_URI"`
	CachePassword string `env:"CACHE_PASSWORD"`
	CacheUseTLS   bool   `env:"CACHE_USE_TLS"`

	MessagingProvider        string `env:"MESSAGING_PROVIDER"`
	MessagingUser            string `env:"MESSAGING_USER"`
	MessagingPassword        string `env:"MESSAGING_PASSWORD"`
	MessagingHost            string `env:"MESSAGING_HOST"`
	MessagingPort            int    `env:"MESSAGING_PORT"`
	MessagingUseTLS          bool   `env:"MESSAGING_USE_TLS"`
	MessagingSASLMechanism   string `env:"MESSAGING_SASL_MECHANISM"`
	MessagingPollingInterval int    `env:"MESSAGING_POLLING_INTERVAL"`

	// OCI-specific messaging credentials. Use Messaging().OCI* for new code.
	MessagingOCITenancyId   string `env:"MESSAGING_OCI_TENANCY_ID"`
	MessagingOCIUserId      string `env:"MESSAGING_OCI_USER_ID"`
	MessagingOCIRegion      string `env:"MESSAGING_OCI_REGION"`
	MessagingOCIFingerPrint string `env:"MESSAGING_OCI_FINGERPRINT"`

	CloudProvider   string `env:"CLOUD_PROVIDER"`
	CloudHost       string `env:"CLOUD_HOST"`
	CloudRegion     string `env:"CLOUD_REGION"`
	CloudSecret     string `env:"CLOUD_SECRET"`
	CloudToken      string `env:"CLOUD_TOKEN"`
	CloudDisableSSL bool   `env:"CLOUD_DISABLE_SSL"`

	// Object-storage bucket configuration. gofi's config.Bucket maps these into
	// the typed bucket.Config.
	BucketProvider string `env:"BUCKET_PROVIDER"`
	BucketName     string `env:"BUCKET_NAME"`
	BucketRegion   string `env:"BUCKET_REGION"`
	BucketEndpoint string `env:"BUCKET_ENDPOINT"`

	// OCI Object Storage credentials.
	BucketOCINamespace   string `env:"BUCKET_OCI_NAMESPACE"`
	BucketOCITenancyID   string `env:"BUCKET_OCI_TENANCY_ID"`
	BucketOCIUserID      string `env:"BUCKET_OCI_USER_ID"`
	BucketOCIFingerPrint string `env:"BUCKET_OCI_FINGERPRINT"`
	BucketOCIPrivateKey  string `env:"BUCKET_OCI_PRIVATE_KEY"`
	BucketOCIPassphrase  string `env:"BUCKET_OCI_PASSPHRASE"`

	// MinIO / S3-compatible credentials.
	BucketS3AccessKey string `env:"BUCKET_S3_ACCESS_KEY"`
	BucketS3SecretKey string `env:"BUCKET_S3_SECRET_KEY"`
	BucketS3UseSSL    bool   `env:"BUCKET_S3_USE_SSL"`

	OtelExporterOTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OtelExporterOTLPHeaders  string `env:"OTEL_EXPORTER_OTLP_HEADERS"`

	// Auth / IAM — universais para qualquer serviço com sessão.
	JWTSecret       string        `env:"JWT_SECRET"`
	JWTIssuer       string        `env:"JWT_ISSUER"`
	AccessTokenTTL  time.Duration `env:"ACCESS_TOKEN_TTL"`
	RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL"`

	// OAuth — provedores externos. Prefixo OAUTH_<PROVIDER>_*.
	OAuthGoogleClientID     string `env:"OAUTH_GOOGLE_CLIENT_ID"`
	OAuthGoogleClientSecret string `env:"OAUTH_GOOGLE_CLIENT_SECRET"`
	OAuthGoogleRedirectURI  string `env:"OAUTH_GOOGLE_REDIRECT_URI"`

	// HTTP / CORS — origens permitidas como CSV ("a,b,c").
	AllowedOrigins string `env:"ALLOWED_ORIGINS"`

	// Mail / SMTP — envio transacional e em massa por qualquer provedor SMTP.
	MailHost       string        `env:"MAIL_HOST"`
	MailPort       int           `env:"MAIL_PORT"`
	MailUsername   string        `env:"MAIL_USERNAME"`
	MailPassword   string        `env:"MAIL_PASSWORD"`
	MailFromName   string        `env:"MAIL_FROM_NAME"`
	MailFromEmail  string        `env:"MAIL_FROM_EMAIL"`
	MailEncryption string        `env:"MAIL_ENCRYPTION"` // none | starttls | tls
	MailAuth       string        `env:"MAIL_AUTH"`       // plain | login | cram-md5 | none
	MailTimeout    time.Duration `env:"MAIL_TIMEOUT"`
	MailMaxRetries int           `env:"MAIL_MAX_RETRIES"`
	MailPoolSize   int           `env:"MAIL_POOL_SIZE"`
	MailHELODomain string        `env:"MAIL_HELO_DOMAIN"`
}

// Defaults aplicados pelo SDK quando o env não traz valor.
const (
	defaultAccessTokenTTL  = 15 * time.Minute
	defaultRefreshTokenTTL = 7 * 24 * time.Hour
)

var (
	environmentInstance *Environment
	once                sync.Once
)

// Instance returns the singleton Environment, initialising it on first call.
func Instance() *Environment {
	once.Do(func() {
		if err := bootstrap(); err != nil {
			slog.Error(
				"failed to bootstrap environment, using defaults",
				slog.String("error", err.Error()),
			)
			environmentInstance = &Environment{}
		}
	})
	return environmentInstance
}

// ResetForTesting resets the singleton so that Instance() re-initialises on
// the next call. Must only be called from tests.
func ResetForTesting() {
	once = sync.Once{}
	environmentInstance = nil
}

func bootstrap() error {
	projectRoot, err := findProjectRoot()
	if err != nil {
		slog.Warn("could not find project root", slog.String("error", err.Error()))
	}

	if projectRoot != "" {
		envPath := filepath.Join(projectRoot, ".env")
		if err := godotenv.Load(envPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w: %w", ErrLoadingEnvironmentFile, err)
		}
	}

	environmentInstance = &Environment{}
	if err := common.ParseStructAnnotation(environmentInstance, "env"); err != nil {
		return fmt.Errorf("%w: %w", ErrParsingEnvironment, err)
	}

	applyEnvironmentConfigurations(environmentInstance)
	return nil
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func applyEnvironmentConfigurations(env *Environment) {
	if !isValidEnvironment(env.AppEnvironment) && env.AppEnvironment != "" {
		slog.Warn(
			"invalid APP_ENVIRONMENT, using default",
			slog.String("value", env.AppEnvironment),
			slog.String("default", string(ENV_DEV)),
		)
	}

	if env.AppMaxParallelWorkers <= 0 {
		env.AppMaxParallelWorkers = APP_MAX_PARALLEL_WORKERS
	}

	// Only validate when a value is explicitly configured.
	if env.CloudProvider != "" && !isValidCloudProvider(env.CloudProvider) {
		slog.Warn("invalid CLOUD_PROVIDER", slog.String("value", env.CloudProvider))
	}

	if env.MessagingProvider != "" && !isValidMessagingProvider(env.MessagingProvider) {
		slog.Warn("invalid MESSAGING_PROVIDER", slog.String("value", env.MessagingProvider))
	}

	if env.CacheType != "" && !isValidCacheType(env.CacheType) {
		slog.Warn("invalid CACHE_TYPE", slog.String("value", env.CacheType))
	}
}

func isValidEnvironment(env string) bool {
	return slices.Contains([]string{
		string(ENV_DEV), string(ENV_STAGE), string(ENV_TEST), string(ENV_PROD),
	}, env)
}

func isValidCloudProvider(provider string) bool {
	return slices.Contains([]string{
		string(CLOUD_AWS), string(CLOUD_GCP), string(CLOUD_OCI), string(CLOUD_NONE),
	}, provider)
}

func isValidMessagingProvider(provider string) bool {
	return slices.Contains([]string{
		string(MESSAGING_RABBITMQ), string(MESSAGING_KAFKA), string(MESSAGING_SQS_SNS),
		string(MESSAGING_AWS), string(MESSAGING_GCP),
	}, provider)
}

func isValidCacheType(cacheType string) bool {
	return slices.Contains([]string{
		string(REDIS_CACHE), string(OCI_CACHE),
	}, cacheType)
}

// --- Environment check helpers ---

func IsEnvironmentDev() bool   { return Instance().GetEnvironmentType() == ENV_DEV }
func IsEnvironmentProd() bool  { return Instance().GetEnvironmentType() == ENV_PROD }
func IsEnvironmentStage() bool { return Instance().GetEnvironmentType() == ENV_STAGE }
func IsEnvironmentTest() bool  { return Instance().GetEnvironmentType() == ENV_TEST }

func IsCloudEnvironment() bool { return IsEnvironmentProd() || IsEnvironmentStage() }
func IsLocalEnvironment() bool { return IsEnvironmentDev() || IsEnvironmentTest() }

// Database DSN construction lives with each sqln driver (Driver.DSN) and is
// wired from these raw fields by gofi's config.Database. The environment only
// reports whether a database is configured (IsDatabaseConfigured).

// =============================================================================
// Phase 4: Typed accessors & convenience methods
// =============================================================================

// GetEnvironmentType returns the strongly-typed environment value.
func (env *Environment) GetEnvironmentType() EnvironmentType {
	return EnvironmentType(env.AppEnvironment)
}

// GetCloudProvider returns the strongly-typed cloud provider.
func (env *Environment) GetCloudProvider() CloudProvider {
	return CloudProvider(env.CloudProvider)
}

// GetMessagingProvider returns the strongly-typed messaging provider.
func (env *Environment) GetMessagingProvider() MessagingProvider {
	return MessagingProvider(env.MessagingProvider)
}

// GetCacheType returns the strongly-typed cache type.
func (env *Environment) GetCacheType() CacheType {
	return CacheType(env.CacheType)
}

// GetLogLevel returns the strongly-typed log level.
func (env *Environment) GetLogLevel() common.LogLevel {
	return common.LogLevel(env.LogLevel)
}

// IsCloudConfigured reports whether a cloud provider is explicitly set and is
// not "none".
func (env *Environment) IsCloudConfigured() bool {
	return env.CloudProvider != "" && env.CloudProvider != string(CLOUD_NONE)
}

// IsMessagingConfigured reports whether a messaging provider is set.
func (env *Environment) IsMessagingConfigured() bool {
	return env.MessagingProvider != ""
}

// IsCacheConfigured reports whether a cache type is set.
func (env *Environment) IsCacheConfigured() bool {
	return env.CacheType != ""
}

// IsDatabaseConfigured reports whether at minimum a driver and name/host are
// present.
func (env *Environment) IsDatabaseConfigured() bool {
	return env.DatabaseDriver != "" && (env.DatabaseName != "" || env.DatabaseHost != "")
}

// =============================================================================
// Phase 5: Segregated config structs
// =============================================================================

// Cache, messaging and cloud configuration is assembled into each library's
// own typed Config by gofi's config package (config.Cloud, config.Kafka,
// config.ConfigureCache, …) directly from the raw fields above. The strongly
// typed Get* accessors below remain for convenience.

// ObservabilityConfig groups all OpenTelemetry configuration.
type ObservabilityConfig struct {
	OTLPEndpoint string
	OTLPHeaders  string
}

// Observability returns an ObservabilityConfig populated from the environment.
func (env *Environment) Observability() ObservabilityConfig {
	return ObservabilityConfig{
		OTLPEndpoint: env.OtelExporterOTLPEndpoint,
		OTLPHeaders:  env.OtelExporterOTLPHeaders,
	}
}

// =============================================================================
// Phase 6: Auth / OAuth / HTTP — universal building blocks for HTTP services.
// =============================================================================

// AuthConfig groups the fields a JWT-based authentication flow needs.
// Returned by Environment.Auth(). Values come from JWT_SECRET, JWT_ISSUER,
// ACCESS_TOKEN_TTL and REFRESH_TOKEN_TTL — defaults applied when missing.
type AuthConfig struct {
	JWTSecret       []byte
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Auth returns an AuthConfig populated from the environment, applying SDK
// defaults for TTLs and falling back to AppName for the issuer when not set.
// Does NOT validate the secret — use RequireAuth() for fail-fast.
func (env *Environment) Auth() AuthConfig {
	issuer := env.JWTIssuer
	if issuer == "" {
		issuer = env.AppName
	}
	access := env.AccessTokenTTL
	if access <= 0 {
		access = defaultAccessTokenTTL
	}
	refresh := env.RefreshTokenTTL
	if refresh <= 0 {
		refresh = defaultRefreshTokenTTL
	}
	return AuthConfig{
		JWTSecret:       []byte(env.JWTSecret),
		Issuer:          issuer,
		AccessTokenTTL:  access,
		RefreshTokenTTL: refresh,
	}
}

// IsAuthConfigured reports whether JWT_SECRET is set. Lets the caller decide
// the policy (warn vs. fatal) instead of forcing it inside the SDK.
func (env *Environment) IsAuthConfigured() bool {
	return env.JWTSecret != ""
}

// RequireAuth returns an error wrapping ErrInvalidEnvironment when JWT_SECRET
// is missing. main.go (or LoadConfig) is the right place to fatalize.
func (env *Environment) RequireAuth() error {
	if env.JWTSecret == "" {
		return fmt.Errorf("%w: JWT_SECRET is required", ErrInvalidEnvironment)
	}
	return nil
}

// OAuthConfig groups OAuth provider configuration. Each provider lives in its
// own field — extend with Microsoft, Apple, OIDC etc. as the SDK supports them.
type OAuthConfig struct {
	Google GoogleOAuthConfig
}

// GoogleOAuthConfig holds Google IDP credentials for OAuth flows.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// OAuth returns an OAuthConfig populated from the environment.
func (env *Environment) OAuth() OAuthConfig {
	return OAuthConfig{
		Google: GoogleOAuthConfig{
			ClientID:     env.OAuthGoogleClientID,
			ClientSecret: env.OAuthGoogleClientSecret,
			RedirectURI:  env.OAuthGoogleRedirectURI,
		},
	}
}

// IsGoogleConfigured reports whether the Google OAuth flow is fully set up.
func (g GoogleOAuthConfig) IsConfigured() bool {
	return g.ClientID != "" && g.ClientSecret != "" && g.RedirectURI != ""
}

// RequireGoogleOAuth returns an error wrapping ErrInvalidEnvironment when any
// of the Google OAuth fields are missing.
func (env *Environment) RequireGoogleOAuth() error {
	g := env.OAuth().Google
	if !g.IsConfigured() {
		return fmt.Errorf(
			"%w: OAUTH_GOOGLE_CLIENT_ID, OAUTH_GOOGLE_CLIENT_SECRET and OAUTH_GOOGLE_REDIRECT_URI are required",
			ErrInvalidEnvironment,
		)
	}
	return nil
}

// HTTPConfig groups HTTP server configuration. AllowedOrigins is the parsed
// CSV from ALLOWED_ORIGINS (each origin already trimmed; empty entries dropped).
type HTTPConfig struct {
	Port           int
	AllowedOrigins []string
}

// HTTP returns an HTTPConfig populated from the environment.
func (env *Environment) HTTP() HTTPConfig {
	return HTTPConfig{
		Port:           env.ServicePort,
		AllowedOrigins: parseAllowedOrigins(env.AllowedOrigins),
	}
}

// parseAllowedOrigins splits a CSV string into trimmed, non-empty entries.
// Returns nil when the input is empty so callers can apply their own default.
func parseAllowedOrigins(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

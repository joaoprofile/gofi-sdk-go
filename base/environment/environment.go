package environment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/common"
	"github.com/joaoprofile/gofi-sdk-go/base/redact"
	"github.com/joaoprofile/gofi-sdk-go/base/secrets"
	"github.com/joho/godotenv"
)

var (
	ErrLoadingEnvironmentFile   = errors.New("error loading environment file")
	ErrParsingEnvironment       = errors.New("error parsing environment variables")
	ErrInvalidEnvironment       = errors.New("invalid environment")
	ErrInvalidMessagingProvider = errors.New("invalid messaging provider")
	ErrInvalidCacheType         = errors.New("invalid cache type")
)

type MessagingProvider string
type CacheType string
type EnvironmentType string

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
	MESSAGING_SQS      MessagingProvider = "sqs"
	MESSAGING_OCI      MessagingProvider = "oci"
	MESSAGING_REDIS    MessagingProvider = "redis"
	MESSAGING_NATS     MessagingProvider = "nats"
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
	AppVersion            string `env:"APP_VERSION"`
	AppEnvironment        string `env:"APP_ENVIRONMENT"`
	AppTenant             int    `env:"APP_TENANT"`
	AppMaxParallelWorkers int    `env:"APP_MAX_PARALLEL_WORKERS"`

	// Timezone is the IANA name applied to time.Local at startup. Empty defaults
	// to Brazil (America/Sao_Paulo).
	Timezone string `env:"TIMEZONE"`

	// TLSInsecureSkipVerify disables certificate checks on http.DefaultTransport.
	// Legacy escape hatch for self-signed endpoints; never enable in production.
	TLSInsecureSkipVerify bool `env:"TLS_INSECURE_SKIP_VERIFY"`

	// AllowInsecureTransport lists the resources (CSV, or "all") that gofi
	// may connect to without verified TLS in prod and stage, with a warning.
	AllowInsecureTransport string `env:"GOFI_ALLOW_INSECURE_TRANSPORT"`

	ServiceDebug     bool   `env:"SERVICE_DEBUG"`
	ServiceDebugAddr string `env:"SERVICE_DEBUG_ADDR"`
	ServiceDebugUser string `env:"SERVICE_DEBUG_USER"`
	ServiceDebugPass string `env:"SERVICE_DEBUG_PASS" redact:"true"`
	ServicePort      int    `env:"PORT"`

	LogLevel  string `env:"LOG_LEVEL"`
	LogOutput string `env:"LOG_OUTPUT"`

	DatabaseDriver       string        `env:"DATABASE_DRIVER"`
	DatabaseHost         string        `env:"DATABASE_HOST"`
	DatabasePort         int           `env:"DATABASE_PORT"`
	DatabaseUser         string        `env:"DATABASE_USER"`
	DatabasePassword     string        `env:"DATABASE_PASSWORD" redact:"true"`
	DatabaseName         string        `env:"DATABASE_NAME"`
	DatabaseSSLMode      string        `env:"DATABASE_SSL_MODE"`
	DatabaseMigration    bool          `env:"DATABASE_MIGRATION"`
	DatabaseMaxOpenConns int           `env:"DATABASE_MAX_OPEN_CONNS"`
	DatabaseMaxIdleConns int           `env:"DATABASE_MAX_IDLE_CONNS"`
	DatabaseMaxLifetime  time.Duration `env:"DATABASE_MAX_LIFETIME"`
	DatabaseMaxIdleTime  time.Duration `env:"DATABASE_MAX_IDLE_TIME"`

	// PEM files: server CA bundle, client certificate and key (mTLS).
	DatabaseSSLRootCert string `env:"DATABASE_SSL_ROOT_CERT"`
	DatabaseSSLCert     string `env:"DATABASE_SSL_CERT"`
	DatabaseSSLKey      string `env:"DATABASE_SSL_KEY"`
	// DatabaseStatementTimeout is enforced by the server; DatabaseQueryTimeout
	// bounds sqln queries without a deadline (0 = sqln default, negative = none).
	DatabaseStatementTimeout time.Duration `env:"DATABASE_STATEMENT_TIMEOUT"`
	DatabaseQueryTimeout     time.Duration `env:"DATABASE_QUERY_TIMEOUT"`
	// Read replica; user, password and database are shared with the primary.
	DatabaseReadHost string `env:"DATABASE_READ_HOST"`
	DatabaseReadPort int    `env:"DATABASE_READ_PORT"`

	CacheType     string `env:"CACHE_TYPE"`
	CacheURI      string `env:"CACHE_URI" redact:"url"`
	CachePassword string `env:"CACHE_PASSWORD" redact:"true"`
	CacheUseTLS   bool   `env:"CACHE_USE_TLS"`

	MessagingProvider        string `env:"MESSAGING_PROVIDER"`
	MessagingUser            string `env:"MESSAGING_USER"`
	MessagingPassword        string `env:"MESSAGING_PASSWORD" redact:"true"`
	MessagingHost            string `env:"MESSAGING_HOST"`
	MessagingPort            int    `env:"MESSAGING_PORT"`
	MessagingUseTLS          bool   `env:"MESSAGING_USE_TLS"`
	MessagingSASLMechanism   string `env:"MESSAGING_SASL_MECHANISM"`
	MessagingPollingInterval int    `env:"MESSAGING_POLLING_INTERVAL"`
	// MessagingEncoding selects the wire format where the broker supports
	// more than one: envelope (default) or cloudevents.
	MessagingEncoding string `env:"MESSAGING_ENCODING"`
	// MessagingRedisMode selects pubsub (default) or streams for the Redis broker.
	MessagingRedisMode string `env:"MESSAGING_REDIS_MODE"`
	// Broker TLS: CA bundle, client certificate and key (PEM files), server
	// name override; any of them enables TLS. Skipping verification is dev only.
	MessagingTLSCAFile             string `env:"MESSAGING_TLS_CA_FILE"`
	MessagingTLSCertFile           string `env:"MESSAGING_TLS_CERT_FILE"`
	MessagingTLSKeyFile            string `env:"MESSAGING_TLS_KEY_FILE"`
	MessagingTLSServerName         string `env:"MESSAGING_TLS_SERVER_NAME"`
	MessagingTLSInsecureSkipVerify bool   `env:"MESSAGING_TLS_INSECURE_SKIP_VERIFY"`
	// MessagingAllowPlaintextSASL lets Kafka send SASL PLAIN without TLS (dev only).
	MessagingAllowPlaintextSASL bool `env:"MESSAGING_ALLOW_PLAINTEXT_SASL"`
	// Service-wide consumer defaults (0 = msq default).
	MessagingMaxDeliveries  int           `env:"MESSAGING_MAX_DELIVERIES"`
	MessagingHandlerTimeout time.Duration `env:"MESSAGING_HANDLER_TIMEOUT"`

	// OCI Queue identity; MESSAGING_OCI_AUTH_MODE selects api_key (default),
	// instance_principal, resource_principal or workload_identity.
	MessagingOCIAuthMode    string `env:"MESSAGING_OCI_AUTH_MODE"`
	MessagingOCITenancyId   string `env:"MESSAGING_OCI_TENANCY_ID"`
	MessagingOCIUserId      string `env:"MESSAGING_OCI_USER_ID"`
	MessagingOCIRegion      string `env:"MESSAGING_OCI_REGION"`
	MessagingOCIFingerPrint string `env:"MESSAGING_OCI_FINGERPRINT"`
	MessagingOCIPrivateKey  string `env:"MESSAGING_OCI_PRIVATE_KEY" redact:"true"`

	// OCIPrivateKey is the shared OCI API key fallback (OCI_PRIVATE_KEY).
	OCIPrivateKey string `env:"OCI_PRIVATE_KEY" redact:"true"`

	// Object-storage bucket configuration. gofi's config.Bucket maps these into
	// the typed bucket.Config.
	BucketProvider string `env:"BUCKET_PROVIDER"`
	BucketName     string `env:"BUCKET_NAME"`
	BucketRegion   string `env:"BUCKET_REGION"`
	BucketEndpoint string `env:"BUCKET_ENDPOINT"`

	// OCI Object Storage credentials. BUCKET_OCI_AUTH_MODE selects the
	// credential source (empty/"api_key", "instance_principal",
	// "resource_principal", "workload_identity"); the key fields below are
	// consumed only by the api_key mode.
	BucketOCIAuthMode    string `env:"BUCKET_OCI_AUTH_MODE"`
	BucketOCINamespace   string `env:"BUCKET_OCI_NAMESPACE"`
	BucketOCITenancyID   string `env:"BUCKET_OCI_TENANCY_ID"`
	BucketOCIUserID      string `env:"BUCKET_OCI_USER_ID"`
	BucketOCIFingerPrint string `env:"BUCKET_OCI_FINGERPRINT"`
	BucketOCIPrivateKey  string `env:"BUCKET_OCI_PRIVATE_KEY" redact:"true"`
	BucketOCIPassphrase  string `env:"BUCKET_OCI_PASSPHRASE" redact:"true"`

	// MinIO / S3-compatible credentials.
	BucketS3AccessKey string `env:"BUCKET_S3_ACCESS_KEY"`
	BucketS3SecretKey string `env:"BUCKET_S3_SECRET_KEY" redact:"true"`
	BucketS3UseSSL    bool   `env:"BUCKET_S3_USE_SSL"`

	// BucketPresignMaxTTL lowers the longest presigned URL validity (max 7 days).
	BucketPresignMaxTTL time.Duration `env:"BUCKET_PRESIGN_MAX_TTL"`

	OtelExporterOTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OtelExporterOTLPHeaders  string `env:"OTEL_EXPORTER_OTLP_HEADERS" redact:"true"`
	// OtelExporterOTLPInsecure follows the OTel convention: "false" enables TLS.
	OtelExporterOTLPInsecure string `env:"OTEL_EXPORTER_OTLP_INSECURE"`
	// OTLP TLS files (PEM paths): trusted CA bundle and mTLS client pair.
	OtelExporterOTLPCertificate       string `env:"OTEL_EXPORTER_OTLP_CERTIFICATE"`
	OtelExporterOTLPClientCertificate string `env:"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE"`
	OtelExporterOTLPClientKey         string `env:"OTEL_EXPORTER_OTLP_CLIENT_KEY"`
	// OtelLegacyMetricNames keeps the pre-v0.3 gofi_* metric names.
	OtelLegacyMetricNames bool `env:"GOFI_OTEL_LEGACY_METRIC_NAMES"`

	// Auth / IAM — universal to any service that has sessions.
	JWTSecret string `env:"JWT_SECRET" redact:"true"`
	JWTIssuer string `env:"JWT_ISSUER"`
	// JWTAudience is issued as aud and required on validation when set.
	JWTAudience string `env:"JWT_AUDIENCE"`
	// JWT key rotation (see iam DefaultConfig).
	JWTKeyID          string        `env:"JWT_KEY_ID"`
	JWTPreviousKeyID  string        `env:"JWT_PREVIOUS_KEY_ID"`
	JWTPreviousSecret string        `env:"JWT_PREVIOUS_SECRET" redact:"true"`
	AccessTokenTTL    time.Duration `env:"ACCESS_TOKEN_TTL"`
	RefreshTokenTTL   time.Duration `env:"REFRESH_TOKEN_TTL"`
	// Login throttling (iam): failures per email before a lockout (0 = iam
	// default, negative disables) and the lockout/window duration.
	IAMLoginMaxAttempts int           `env:"IAM_LOGIN_MAX_ATTEMPTS"`
	IAMLoginLockout     time.Duration `env:"IAM_LOGIN_LOCKOUT"`

	// OAuth — provedores externos. Prefixo OAUTH_<PROVIDER>_*.
	OAuthGoogleClientID     string `env:"OAUTH_GOOGLE_CLIENT_ID"`
	OAuthGoogleClientSecret string `env:"OAUTH_GOOGLE_CLIENT_SECRET" redact:"true"`
	OAuthGoogleRedirectURI  string `env:"OAUTH_GOOGLE_REDIRECT_URI"`

	// HTTP / CORS — origens permitidas como CSV ("a,b,c").
	AllowedOrigins string `env:"ALLOWED_ORIGINS"`

	// HTTP server (gofi httpserver component); code-provided config wins.
	HTTPTLSCertFile     string `env:"HTTP_TLS_CERT_FILE"`
	HTTPTLSKeyFile      string `env:"HTTP_TLS_KEY_FILE"`
	HTTPTLSClientCAFile string `env:"HTTP_TLS_CLIENT_CA_FILE"`
	// HTTPTLSClientAuth: none | request | require | verify_if_given | require_and_verify.
	HTTPTLSClientAuth string `env:"HTTP_TLS_CLIENT_AUTH"`
	// HTTPTrustedProxies is a CSV of CIDRs; "private" expands to the private ranges.
	HTTPTrustedProxies      string `env:"HTTP_TRUSTED_PROXIES"`
	HTTPMaxConcurrent       int    `env:"HTTP_MAX_CONCURRENT"`
	HTTPRateLimitFailClosed bool   `env:"HTTP_RATE_LIMIT_FAIL_CLOSED"`
	// HTTPAllowedOrigins is a CSV of CORS origins; ALLOWED_ORIGINS is the fallback.
	HTTPAllowedOrigins string `env:"HTTP_ALLOWED_ORIGINS"`
	// HTTPRequireTLS makes a plaintext HTTP server a refusal in prod and stage
	// instead of a warning.
	HTTPRequireTLS bool `env:"HTTP_REQUIRE_TLS"`

	// gRPC server (gofi grpcserver component); code-provided config wins.
	GRPCTLSCertFile     string `env:"GRPC_TLS_CERT_FILE"`
	GRPCTLSKeyFile      string `env:"GRPC_TLS_KEY_FILE"`
	GRPCTLSClientCAFile string `env:"GRPC_TLS_CLIENT_CA_FILE"`
	// GRPCTLSClientAuth: none | request | require | verify_if_given | require_and_verify.
	GRPCTLSClientAuth string `env:"GRPC_TLS_CLIENT_AUTH"`
	// GRPCRequireTLS makes a plaintext gRPC server a refusal in prod and stage
	// instead of a warning.
	GRPCRequireTLS bool `env:"GRPC_REQUIRE_TLS"`

	// Mail / SMTP — envio transacional e em massa por qualquer provedor SMTP.
	MailHost       string        `env:"MAIL_HOST"`
	MailPort       int           `env:"MAIL_PORT"`
	MailUsername   string        `env:"MAIL_USERNAME"`
	MailPassword   string        `env:"MAIL_PASSWORD" redact:"true"`
	MailFromName   string        `env:"MAIL_FROM_NAME"`
	MailFromEmail  string        `env:"MAIL_FROM_EMAIL"`
	MailEncryption string        `env:"MAIL_ENCRYPTION"` // none | starttls | tls
	MailAuth       string        `env:"MAIL_AUTH"`       // plain | login | cram-md5 | none
	MailTimeout    time.Duration `env:"MAIL_TIMEOUT"`
	MailMaxRetries int           `env:"MAIL_MAX_RETRIES"`
	MailPoolSize   int           `env:"MAIL_POOL_SIZE"`
	MailHELODomain string        `env:"MAIL_HELO_DOMAIN"`
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (env Environment) String() string                { return redact.Sprint(env) }
func (env Environment) GoString() string              { return redact.GoSprint(env) }
func (env Environment) Format(f fmt.State, verb rune) { redact.Format(f, verb, env) }
func (env Environment) LogValue() slog.Value          { return redact.LogValue(env) }
func (env Environment) MarshalJSON() ([]byte, error)  { return redact.JSON(env) }

// Defaults applied by the SDK when the environment carries no value.
const (
	defaultAccessTokenTTL  = 15 * time.Minute
	defaultRefreshTokenTTL = 7 * 24 * time.Hour
)

var (
	environmentInstance *Environment
	loadErr             error
	once                sync.Once
)

// Instance returns the singleton Environment, loading it on first call. Load
// errors do not panic: the partially loaded Environment is returned and the
// error is available through LoadError (gofi's Build returns it).
func Instance() *Environment {
	once.Do(func() {
		environmentInstance, loadErr = Load()
		if loadErr != nil {
			slog.Error("environment loaded with errors", slog.Any("error", loadErr))
		}
	})
	return environmentInstance
}

// LoadError returns the error of the Instance load, if any.
func LoadError() error {
	Instance()
	return loadErr
}

// ResetForTesting resets the singleton so that Instance() re-initialises on
// the next call. Must only be called from tests.
func ResetForTesting() {
	once = sync.Once{}
	environmentInstance = nil
	loadErr = nil
}

// Load reads the environment into a new Environment and returns every problem
// joined. The .env file is only read outside production (see shouldLoadDotEnv),
// each variable X can be supplied as a file path in X_FILE (mounted secrets),
// and a value of the form secret://<provider>/<name>[#key] is fetched from
// that secret manager (see package secrets).
func Load() (*Environment, error) {
	var errs []error
	if shouldLoadDotEnv() {
		if err := loadDotEnv(); err != nil {
			errs = append(errs, err)
		}
	}

	env := &Environment{}
	if err := common.ParseStructAnnotationFunc(env, "env", withSecrets(lookupWithFile(&errs), &errs)); err != nil {
		errs = append(errs, fmt.Errorf("%w: %w", ErrParsingEnvironment, err))
	}
	if err := applyEnvironmentConfigurations(env); err != nil {
		errs = append(errs, err)
	}
	return env, errors.Join(errs...)
}

// shouldLoadDotEnv reads .env only when APP_ENVIRONMENT is empty, dev or test;
// stage, prod and unknown values (e.g. "production") take configuration from
// the platform. GOFI_DOTENV=true|false overrides.
func shouldLoadDotEnv() bool {
	if v, err := strconv.ParseBool(os.Getenv("GOFI_DOTENV")); err == nil {
		return v
	}
	switch os.Getenv("APP_ENVIRONMENT") {
	case "", string(ENV_DEV), string(ENV_TEST):
		return true
	}
	return false
}

func loadDotEnv() error {
	projectRoot, err := findProjectRoot()
	if err != nil || projectRoot == "" {
		return nil
	}
	if err := godotenv.Load(filepath.Join(projectRoot, ".env")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: %w", ErrLoadingEnvironmentFile, err)
	}
	return nil
}

// lookupWithFile resolves X from the environment, or from the file named by X_FILE.
func lookupWithFile(errs *[]error) func(string) string {
	return func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		path := os.Getenv(key + "_FILE")
		if path == "" {
			return ""
		}
		data, err := os.ReadFile(path) // #nosec G304 G703 -- *_FILE paths are set by the operator, not by requests
		if err != nil {
			*errs = append(*errs, fmt.Errorf("%s_FILE: %w", key, err))
			return ""
		}
		return strings.TrimRight(string(data), "\r\n")
	}
}

// secretTimeout bounds the startup lookups of secret references.
const secretTimeout = 30 * time.Second

// withSecrets resolves secret:// values returned by lookup; each secret is
// fetched once per Load.
func withSecrets(lookup func(string) string, errs *[]error) func(string) string {
	r := secrets.NewResolver()
	return func(key string) string {
		v := lookup(key)
		if !secrets.IsRef(v) {
			return v
		}
		ctx, cancel := context.WithTimeout(context.Background(), secretTimeout)
		defer cancel()
		resolved, err := r.Resolve(ctx, v)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
			return ""
		}
		return resolved
	}
}

// findProjectRoot returns the directory holding the .env to load: the working
// directory, or its nearest ancestor up to the enclosing Go module root (the
// first directory with a go.mod). It never looks above the module root, and
// outside a module (a deployed binary) only the working directory counts, so
// a stray .env in a parent directory is never picked up.
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := moduleRoot(cwd)
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, ".env")) {
			return dir, nil
		}
		if root == "" || dir == root || filepath.Dir(dir) == dir {
			return "", nil
		}
	}
}

// moduleRoot returns the nearest directory at or above dir with a go.mod, or "".
func moduleRoot(dir string) string {
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// applyEnvironmentConfigurations applies defaults and rejects an unknown
// APP_ENVIRONMENT: guessing dev for a typo like "production" would disable
// every production safeguard.
func applyEnvironmentConfigurations(env *Environment) error {
	var err error
	if env.AppEnvironment != "" && !isValidEnvironment(env.AppEnvironment) {
		err = fmt.Errorf("%w: APP_ENVIRONMENT=%q, want one of dev, stage, test, prod",
			ErrInvalidEnvironment, env.AppEnvironment)
	}

	if env.AppMaxParallelWorkers <= 0 {
		env.AppMaxParallelWorkers = APP_MAX_PARALLEL_WORKERS
	}

	if env.MessagingProvider != "" && !isValidMessagingProvider(env.MessagingProvider) {
		slog.Warn("invalid MESSAGING_PROVIDER", slog.String("value", env.MessagingProvider))
	}

	if env.CacheType != "" && !isValidCacheType(env.CacheType) {
		slog.Warn("invalid CACHE_TYPE", slog.String("value", env.CacheType))
	}
	return err
}

func isValidEnvironment(env string) bool {
	return slices.Contains([]string{
		string(ENV_DEV), string(ENV_STAGE), string(ENV_TEST), string(ENV_PROD),
	}, env)
}

func isValidMessagingProvider(provider string) bool {
	return slices.Contains([]string{
		string(MESSAGING_RABBITMQ), string(MESSAGING_KAFKA), string(MESSAGING_SQS),
		string(MESSAGING_OCI), string(MESSAGING_REDIS), string(MESSAGING_NATS),
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

// Cache and bucket configuration is assembled into each library's own typed
// Config by gofi's config package, directly from the raw fields above. The strongly
// typed Get* accessors below remain for convenience.

// ObservabilityConfig groups all OpenTelemetry configuration.
type ObservabilityConfig struct {
	OTLPEndpoint string
	OTLPHeaders  string `redact:"true"`
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c ObservabilityConfig) String() string                { return redact.Sprint(c) }
func (c ObservabilityConfig) GoString() string              { return redact.GoSprint(c) }
func (c ObservabilityConfig) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c ObservabilityConfig) LogValue() slog.Value          { return redact.LogValue(c) }
func (c ObservabilityConfig) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

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
	JWTSecret       []byte `redact:"true"`
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (c AuthConfig) String() string                { return redact.Sprint(c) }
func (c AuthConfig) GoString() string              { return redact.GoSprint(c) }
func (c AuthConfig) Format(f fmt.State, verb rune) { redact.Format(f, verb, c) }
func (c AuthConfig) LogValue() slog.Value          { return redact.LogValue(c) }
func (c AuthConfig) MarshalJSON() ([]byte, error)  { return redact.JSON(c) }

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
	ClientSecret string `redact:"true"`
	RedirectURI  string
}

// String, GoString, Format, LogValue and MarshalJSON mask the secret fields.
func (g GoogleOAuthConfig) String() string                { return redact.Sprint(g) }
func (g GoogleOAuthConfig) GoString() string              { return redact.GoSprint(g) }
func (g GoogleOAuthConfig) Format(f fmt.State, verb rune) { redact.Format(f, verb, g) }
func (g GoogleOAuthConfig) LogValue() slog.Value          { return redact.LogValue(g) }
func (g GoogleOAuthConfig) MarshalJSON() ([]byte, error)  { return redact.JSON(g) }

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

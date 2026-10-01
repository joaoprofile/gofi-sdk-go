package environment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reset restores singleton + globals between tests.
func reset(t *testing.T) {
	t.Helper()
	t.Cleanup(ResetForTesting)
}

// ---- isValidEnvironment ----

func TestIsValidEnvironment(t *testing.T) {
	valid := []string{"dev", "stage", "test", "prod"}
	for _, v := range valid {
		if !isValidEnvironment(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}

	invalid := []string{"development", "staging", "production", "testing", ""}
	for _, v := range invalid {
		if isValidEnvironment(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

// ---- isValidCacheType ----

func TestIsValidCacheType(t *testing.T) {
	valid := []string{"redis", "oci"}
	for _, v := range valid {
		if !isValidCacheType(v) {
			t.Errorf("expected cache type %q to be valid", v)
		}
	}

	invalid := []string{"memcached", ""}
	for _, v := range invalid {
		if isValidCacheType(v) {
			t.Errorf("expected cache type %q to be invalid", v)
		}
	}
}

// ---- bootstrap ----

func TestBootstrap(t *testing.T) {
	reset(t)

	t.Setenv("APP_ENVIRONMENT", "dev")
	t.Setenv("PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")

	if err := LoadError(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	env := Instance()
	if env.AppEnvironment != "dev" {
		t.Errorf("expected AppEnvironment=dev, got %v", env.AppEnvironment)
	}
	if env.ServicePort != 9090 {
		t.Errorf("expected ServicePort=9090, got %v", env.ServicePort)
	}
	if env.LogLevel != "debug" {
		t.Errorf("expected LogLevel=debug, got %v", env.LogLevel)
	}
}

// ---- environment check helpers ----

func TestEnvironmentCheckFunctions(t *testing.T) {
	cases := []struct {
		name      string
		env       EnvironmentType
		wantDev   bool
		wantProd  bool
		wantStage bool
		wantTest  bool
		wantCloud bool
		wantLocal bool
	}{
		{"dev", ENV_DEV, true, false, false, false, false, true},
		{"prod", ENV_PROD, false, true, false, false, true, false},
		{"stage", ENV_STAGE, false, false, true, false, true, false},
		{"test", ENV_TEST, false, false, false, true, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetForTesting()
			t.Cleanup(ResetForTesting)
			t.Setenv("APP_ENVIRONMENT", string(tc.env))

			if got := IsEnvironmentDev(); got != tc.wantDev {
				t.Errorf("IsEnvironmentDev()=%v, want %v", got, tc.wantDev)
			}
			if got := IsEnvironmentProd(); got != tc.wantProd {
				t.Errorf("IsEnvironmentProd()=%v, want %v", got, tc.wantProd)
			}
			if got := IsEnvironmentStage(); got != tc.wantStage {
				t.Errorf("IsEnvironmentStage()=%v, want %v", got, tc.wantStage)
			}
			if got := IsEnvironmentTest(); got != tc.wantTest {
				t.Errorf("IsEnvironmentTest()=%v, want %v", got, tc.wantTest)
			}
			if got := IsCloudEnvironment(); got != tc.wantCloud {
				t.Errorf("IsCloudEnvironment()=%v, want %v", got, tc.wantCloud)
			}
			if got := IsLocalEnvironment(); got != tc.wantLocal {
				t.Errorf("IsLocalEnvironment()=%v, want %v", got, tc.wantLocal)
			}
		})
	}
}

// DSN construction is tested per driver (sqln/driver/*) and end-to-end via the
// config package (config.Database); the environment no longer assembles DSNs.

// ---- typed accessors ----

func TestTypedAccessors(t *testing.T) {
	env := Environment{
		AppEnvironment:    "prod",
		MessagingProvider: "rabbitmq",
		CacheType:         "redis",
		LogLevel:          "debug",
	}

	if got := env.GetEnvironmentType(); got != ENV_PROD {
		t.Errorf("GetEnvironmentType()=%q, want %q", got, ENV_PROD)
	}
	if got := env.GetMessagingProvider(); got != MESSAGING_RABBITMQ {
		t.Errorf("GetMessagingProvider()=%q, want %q", got, MESSAGING_RABBITMQ)
	}
	if got := env.GetCacheType(); got != REDIS_CACHE {
		t.Errorf("GetCacheType()=%q, want %q", got, REDIS_CACHE)
	}
}

// ---- IsXxxConfigured ----

func TestIsConfigured(t *testing.T) {
	t.Run("all empty", func(t *testing.T) {
		env := Environment{}
		if env.IsMessagingConfigured() {
			t.Error("expected IsMessagingConfigured()=false for empty provider")
		}
		if env.IsCacheConfigured() {
			t.Error("expected IsCacheConfigured()=false for empty type")
		}
		if env.IsDatabaseConfigured() {
			t.Error("expected IsDatabaseConfigured()=false for empty config")
		}
	})

	t.Run("configured", func(t *testing.T) {
		env := Environment{
			MessagingProvider: "kafka",
			CacheType:         "redis",
			DatabaseDriver:    "postgres",
			DatabaseHost:      "localhost",
		}
		if !env.IsMessagingConfigured() {
			t.Error("expected IsMessagingConfigured()=true")
		}
		if !env.IsCacheConfigured() {
			t.Error("expected IsCacheConfigured()=true")
		}
		if !env.IsDatabaseConfigured() {
			t.Error("expected IsDatabaseConfigured()=true")
		}
	})
}

// ---- segregated config structs ----

func TestObservabilityConfig(t *testing.T) {
	env := Environment{
		OtelExporterOTLPEndpoint: "http://otel:4318",
		OtelExporterOTLPHeaders:  "Authorization=Bearer token",
	}

	cfg := env.Observability()
	if cfg.OTLPEndpoint != "http://otel:4318" {
		t.Errorf("OTLPEndpoint=%q, want http://otel:4318", cfg.OTLPEndpoint)
	}
}

// ---- ResetForTesting ----

func TestResetForTesting(t *testing.T) {
	t.Setenv("APP_ENVIRONMENT", "prod")
	_ = Instance() // trigger bootstrap

	ResetForTesting()

	if environmentInstance != nil {
		t.Error("expected nil instance after ResetForTesting")
	}
}

// ---- AuthConfig ----

func TestAuthConfig_AppliesDefaults(t *testing.T) {
	env := Environment{
		AppName:   "billing-service",
		JWTSecret: "shh",
	}

	cfg := env.Auth()
	if string(cfg.JWTSecret) != "shh" {
		t.Errorf("JWTSecret=%q, want %q", cfg.JWTSecret, "shh")
	}
	if cfg.Issuer != "billing-service" {
		t.Errorf("Issuer=%q, want fallback to AppName 'billing-service'", cfg.Issuer)
	}
	if cfg.AccessTokenTTL != defaultAccessTokenTTL {
		t.Errorf("AccessTokenTTL=%v, want default %v", cfg.AccessTokenTTL, defaultAccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != defaultRefreshTokenTTL {
		t.Errorf("RefreshTokenTTL=%v, want default %v", cfg.RefreshTokenTTL, defaultRefreshTokenTTL)
	}
}

func TestAuthConfig_OverridesDefaults(t *testing.T) {
	env := Environment{
		AppName:         "x",
		JWTSecret:       "s",
		JWTIssuer:       "explicit-issuer",
		AccessTokenTTL:  30 * time.Minute,
		RefreshTokenTTL: 14 * 24 * time.Hour,
	}

	cfg := env.Auth()
	if cfg.Issuer != "explicit-issuer" {
		t.Errorf("Issuer=%q, want explicit-issuer", cfg.Issuer)
	}
	if cfg.AccessTokenTTL != 30*time.Minute {
		t.Errorf("AccessTokenTTL=%v, want 30m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 14*24*time.Hour {
		t.Errorf("RefreshTokenTTL=%v, want 336h", cfg.RefreshTokenTTL)
	}
}

func TestAuthConfig_NegativeTTLFallsBackToDefault(t *testing.T) {
	env := Environment{JWTSecret: "x", AccessTokenTTL: -1 * time.Second}
	if got := env.Auth().AccessTokenTTL; got != defaultAccessTokenTTL {
		t.Errorf("AccessTokenTTL=%v, want default %v on negative input", got, defaultAccessTokenTTL)
	}
}

func TestIsAuthConfigured(t *testing.T) {
	if (&Environment{}).IsAuthConfigured() {
		t.Error("expected IsAuthConfigured()=false on empty env")
	}
	if !(&Environment{JWTSecret: "x"}).IsAuthConfigured() {
		t.Error("expected IsAuthConfigured()=true when JWTSecret set")
	}
}

func TestRequireAuth_MissingSecret(t *testing.T) {
	err := (&Environment{}).RequireAuth()
	if err == nil {
		t.Fatal("expected error when JWTSecret empty")
	}
	if !errors.Is(err, ErrInvalidEnvironment) {
		t.Errorf("expected error to wrap ErrInvalidEnvironment, got %v", err)
	}
}

func TestRequireAuth_OK(t *testing.T) {
	if err := (&Environment{JWTSecret: "x"}).RequireAuth(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- OAuthConfig ----

func TestOAuthConfig_GoogleEmpty(t *testing.T) {
	cfg := (&Environment{}).OAuth()
	if cfg.Google.IsConfigured() {
		t.Error("expected Google.IsConfigured()=false on empty env")
	}
}

func TestOAuthConfig_GooglePopulated(t *testing.T) {
	env := Environment{
		OAuthGoogleClientID:     "id",
		OAuthGoogleClientSecret: "secret",
		OAuthGoogleRedirectURI:  "http://localhost/cb",
	}
	g := env.OAuth().Google
	if !g.IsConfigured() {
		t.Error("expected Google.IsConfigured()=true when all 3 fields set")
	}
	if g.ClientID != "id" || g.ClientSecret != "secret" || g.RedirectURI != "http://localhost/cb" {
		t.Errorf("OAuth values mismatched: %+v", g)
	}
}

func TestOAuthConfig_GooglePartialIsNotConfigured(t *testing.T) {
	cases := []Environment{
		{OAuthGoogleClientSecret: "s", OAuthGoogleRedirectURI: "r"},
		{OAuthGoogleClientID: "i", OAuthGoogleRedirectURI: "r"},
		{OAuthGoogleClientID: "i", OAuthGoogleClientSecret: "s"},
	}
	for i, env := range cases {
		if env.OAuth().Google.IsConfigured() {
			t.Errorf("case %d: expected partial config to be IsConfigured()=false", i)
		}
	}
}

func TestRequireGoogleOAuth_Missing(t *testing.T) {
	err := (&Environment{OAuthGoogleClientID: "id"}).RequireGoogleOAuth()
	if !errors.Is(err, ErrInvalidEnvironment) {
		t.Errorf("expected ErrInvalidEnvironment, got %v", err)
	}
}

func TestRequireGoogleOAuth_OK(t *testing.T) {
	env := &Environment{
		OAuthGoogleClientID:     "i",
		OAuthGoogleClientSecret: "s",
		OAuthGoogleRedirectURI:  "r",
	}
	if err := env.RequireGoogleOAuth(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- HTTPConfig ----

func TestHTTPConfig_Empty(t *testing.T) {
	cfg := (&Environment{}).HTTP()
	if cfg.Port != 0 {
		t.Errorf("Port=%d, want 0 on empty env", cfg.Port)
	}
	if cfg.AllowedOrigins != nil {
		t.Errorf("AllowedOrigins=%v, want nil", cfg.AllowedOrigins)
	}
}

func TestHTTPConfig_PortFromServicePort(t *testing.T) {
	env := Environment{ServicePort: 9090}
	if got := env.HTTP().Port; got != 9090 {
		t.Errorf("Port=%d, want 9090", got)
	}
}

func TestParseAllowedOrigins(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "http://a.com", []string{"http://a.com"}},
		{"multi", "http://a.com,http://b.com", []string{"http://a.com", "http://b.com"}},
		{"trims whitespace", "  http://a.com , http://b.com  ", []string{"http://a.com", "http://b.com"}},
		{"drops empty entries", "http://a.com,,http://b.com", []string{"http://a.com", "http://b.com"}},
		{"trailing comma", "http://a.com,", []string{"http://a.com"}},
		{"only commas → nil", ",,,", nil},
		{"only whitespace → nil", "   ", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAllowedOrigins(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d]=%q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// HTTP() must allocate a fresh slice each call so callers can't mutate
// shared state across invocations.
func TestHTTPConfig_AllowedOriginsIsolation(t *testing.T) {
	env := Environment{AllowedOrigins: "http://a.com,http://b.com"}
	a := env.HTTP().AllowedOrigins
	a[0] = "MUTATED"
	b := env.HTTP().AllowedOrigins
	if b[0] == "MUTATED" {
		t.Error("HTTP() must allocate a fresh slice each call; mutation leaked")
	}
}

// End-to-end parser: catches tag-name typos on the new struct fields.
func TestEnvironmentNewFieldsBootstrap(t *testing.T) {
	reset(t)
	t.Setenv("JWT_SECRET", "x")
	t.Setenv("JWT_ISSUER", "iss")
	t.Setenv("ACCESS_TOKEN_TTL", "1m")
	t.Setenv("REFRESH_TOKEN_TTL", "1h")
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "gid")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "gsec")
	t.Setenv("OAUTH_GOOGLE_REDIRECT_URI", "http://x/cb")
	t.Setenv("ALLOWED_ORIGINS", "http://a, http://b")
	t.Setenv("IAM_LOGIN_MAX_ATTEMPTS", "7")
	t.Setenv("IAM_LOGIN_LOCKOUT", "20m")

	env := Instance()
	if env.IAMLoginMaxAttempts != 7 || env.IAMLoginLockout != 20*time.Minute {
		t.Errorf("IAM login throttling not loaded: %d / %v", env.IAMLoginMaxAttempts, env.IAMLoginLockout)
	}
	if env.JWTSecret != "x" || env.JWTIssuer != "iss" {
		t.Errorf("JWT vars not loaded: %+v", env)
	}
	if env.AccessTokenTTL != time.Minute || env.RefreshTokenTTL != time.Hour {
		t.Errorf("TTLs not loaded: %v / %v", env.AccessTokenTTL, env.RefreshTokenTTL)
	}
	if env.OAuthGoogleClientID != "gid" || env.OAuthGoogleClientSecret != "gsec" || env.OAuthGoogleRedirectURI != "http://x/cb" {
		t.Errorf("OAuth vars not loaded: %+v", env)
	}
	origins := env.HTTP().AllowedOrigins
	if len(origins) != 2 || origins[0] != "http://a" || origins[1] != "http://b" {
		t.Errorf("AllowedOrigins not parsed correctly: %v", origins)
	}
}

// End-to-end parser for the HTTP server fields.
func TestEnvironmentHTTPServerFieldsBootstrap(t *testing.T) {
	reset(t)
	vars := map[string]string{
		"HTTP_TLS_CERT_FILE":          "/tls/tls.crt",
		"HTTP_TLS_KEY_FILE":           "/tls/tls.key",
		"HTTP_TLS_CLIENT_CA_FILE":     "/tls/ca.crt",
		"HTTP_TLS_CLIENT_AUTH":        "require_and_verify",
		"HTTP_TRUSTED_PROXIES":        "private,203.0.113.0/24",
		"HTTP_MAX_CONCURRENT":         "512",
		"HTTP_RATE_LIMIT_FAIL_CLOSED": "true",
		"HTTP_ALLOWED_ORIGINS":        "https://a.example.com",
		"HTTP_REQUIRE_TLS":            "true",
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}

	env := Instance()
	got := Environment{
		HTTPTLSCertFile: env.HTTPTLSCertFile, HTTPTLSKeyFile: env.HTTPTLSKeyFile,
		HTTPTLSClientCAFile: env.HTTPTLSClientCAFile, HTTPTLSClientAuth: env.HTTPTLSClientAuth,
		HTTPTrustedProxies: env.HTTPTrustedProxies, HTTPMaxConcurrent: env.HTTPMaxConcurrent,
		HTTPRateLimitFailClosed: env.HTTPRateLimitFailClosed, HTTPAllowedOrigins: env.HTTPAllowedOrigins,
		HTTPRequireTLS: env.HTTPRequireTLS,
	}
	want := Environment{
		HTTPTLSCertFile: "/tls/tls.crt", HTTPTLSKeyFile: "/tls/tls.key",
		HTTPTLSClientCAFile: "/tls/ca.crt", HTTPTLSClientAuth: "require_and_verify",
		HTTPTrustedProxies: "private,203.0.113.0/24", HTTPMaxConcurrent: 512,
		HTTPRateLimitFailClosed: true, HTTPAllowedOrigins: "https://a.example.com",
		HTTPRequireTLS: true,
	}
	if got != want {
		t.Errorf("HTTP_* not loaded:\n got %#v\nwant %#v", got, want)
	}
}

// End-to-end parser for the OTLP TLS file fields.
func TestEnvironmentOTLPTLSFieldsBootstrap(t *testing.T) {
	reset(t)
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", "/tls/ca.crt")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", "/tls/client.crt")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_KEY", "/tls/client.key")

	env := Instance()
	if env.OtelExporterOTLPCertificate != "/tls/ca.crt" ||
		env.OtelExporterOTLPClientCertificate != "/tls/client.crt" ||
		env.OtelExporterOTLPClientKey != "/tls/client.key" {
		t.Errorf("OTEL_EXPORTER_OTLP_* TLS files not loaded: %q %q %q",
			env.OtelExporterOTLPCertificate, env.OtelExporterOTLPClientCertificate, env.OtelExporterOTLPClientKey)
	}
}

// End-to-end parser for the transport security and timeout fields.
func TestEnvironmentSecurityFieldsBootstrap(t *testing.T) {
	reset(t)
	t.Setenv("GOFI_ALLOW_INSECURE_TRANSPORT", "database,cache")
	t.Setenv("DATABASE_SSL_ROOT_CERT", "/ca.pem")
	t.Setenv("DATABASE_SSL_CERT", "/c.pem")
	t.Setenv("DATABASE_SSL_KEY", "/k.pem")
	t.Setenv("DATABASE_STATEMENT_TIMEOUT", "5s")
	t.Setenv("DATABASE_QUERY_TIMEOUT", "10s")
	t.Setenv("MESSAGING_TLS_CA_FILE", "/mca.pem")
	t.Setenv("MESSAGING_TLS_CERT_FILE", "/mc.pem")
	t.Setenv("MESSAGING_TLS_KEY_FILE", "/mk.pem")
	t.Setenv("MESSAGING_TLS_SERVER_NAME", "mq.internal")
	t.Setenv("MESSAGING_TLS_INSECURE_SKIP_VERIFY", "true")
	t.Setenv("MESSAGING_ALLOW_PLAINTEXT_SASL", "true")
	t.Setenv("MESSAGING_MAX_DELIVERIES", "7")
	t.Setenv("MESSAGING_HANDLER_TIMEOUT", "1m")
	t.Setenv("JWT_AUDIENCE", "billing-api")

	env := Instance()
	if err := LoadError(); err != nil {
		t.Fatalf("LoadError: %v", err)
	}
	if env.AllowInsecureTransport != "database,cache" || env.JWTAudience != "billing-api" {
		t.Errorf("guard/audience not loaded: %q %q", env.AllowInsecureTransport, env.JWTAudience)
	}
	if env.DatabaseSSLRootCert != "/ca.pem" || env.DatabaseSSLCert != "/c.pem" || env.DatabaseSSLKey != "/k.pem" ||
		env.DatabaseStatementTimeout != 5*time.Second || env.DatabaseQueryTimeout != 10*time.Second {
		t.Errorf("database vars not loaded")
	}
	if env.MessagingTLSCAFile != "/mca.pem" || env.MessagingTLSCertFile != "/mc.pem" || env.MessagingTLSKeyFile != "/mk.pem" ||
		env.MessagingTLSServerName != "mq.internal" || !env.MessagingTLSInsecureSkipVerify || !env.MessagingAllowPlaintextSASL ||
		env.MessagingMaxDeliveries != 7 || env.MessagingHandlerTimeout != time.Minute {
		t.Errorf("messaging vars not loaded")
	}
}

// ---- GetLogLevel ----

func TestGetLogLevel(t *testing.T) {
	env := Environment{LogLevel: "debug"}
	if got := env.GetLogLevel(); string(got) != "debug" {
		t.Errorf("GetLogLevel()=%q, want %q", got, "debug")
	}

	env2 := Environment{LogLevel: "error"}
	if got := env2.GetLogLevel(); string(got) != "error" {
		t.Errorf("GetLogLevel()=%q, want %q", got, "error")
	}
}

// ---- applyEnvironmentConfigurations warning paths ----

func TestApplyEnvironmentConfigurationsWarnings(t *testing.T) {
	env := &Environment{
		AppEnvironment:        "invalid-env-type",  // triggers invalid-env warning
		AppMaxParallelWorkers: -1,                  // triggers default reset
		MessagingProvider:     "invalid-messaging", // triggers invalid-messaging warning
		CacheType:             "invalid-cache",     // triggers invalid-cache warning
	}
	if err := applyEnvironmentConfigurations(env); !errors.Is(err, ErrInvalidEnvironment) {
		t.Errorf("invalid APP_ENVIRONMENT: err=%v, want ErrInvalidEnvironment", err)
	}

	if env.AppMaxParallelWorkers != APP_MAX_PARALLEL_WORKERS {
		t.Errorf("Expected AppMaxParallelWorkers=%d after reset, got %d",
			APP_MAX_PARALLEL_WORKERS, env.AppMaxParallelWorkers)
	}
}

func TestApplyEnvironmentConfigurationsZeroWorkers(t *testing.T) {
	env := &Environment{AppMaxParallelWorkers: 0}
	applyEnvironmentConfigurations(env)
	if env.AppMaxParallelWorkers != APP_MAX_PARALLEL_WORKERS {
		t.Errorf("Expected AppMaxParallelWorkers=%d, got %d", APP_MAX_PARALLEL_WORKERS, env.AppMaxParallelWorkers)
	}
}

// ---- findProjectRoot ----

func TestFindProjectRootWithEnvFile(t *testing.T) {
	dir := t.TempDir()

	// Plant a .env file so findProjectRoot can find it.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	root, err := findProjectRoot()
	if err != nil {
		t.Fatalf("findProjectRoot() unexpected error: %v", err)
	}
	if root != dir {
		t.Errorf("findProjectRoot()=%q, want %q", root, dir)
	}
}

// ---- Instance bootstrap failure ----

func TestInstanceBootstrapFailure(t *testing.T) {
	ResetForTesting()
	t.Cleanup(ResetForTesting)

	// PORT must parse as int — set it to an invalid value so
	// ParseStructAnnotation fails and bootstrap returns an error.
	t.Setenv("PORT", "not-a-port-number")

	env := Instance()
	if env == nil {
		t.Error("Expected non-nil Environment even after bootstrap failure")
	}
}

func TestLoad_ReportsEveryInvalidVariable(t *testing.T) {
	t.Setenv("GOFI_DOTENV", "false")
	t.Setenv("PORT", "not-a-number")
	t.Setenv("DATABASE_MIGRATION", "maybe")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "PORT") || !strings.Contains(err.Error(), "DATABASE_MIGRATION") {
		t.Fatalf("both invalid variables must be reported, got %v", err)
	}
}

func TestLoad_FileSuffixReadsMountedSecret(t *testing.T) {
	t.Setenv("GOFI_DOTENV", "false")
	path := filepath.Join(t.TempDir(), "db-password")
	if err := os.WriteFile(path, []byte("s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_PASSWORD", "")
	t.Setenv("DATABASE_PASSWORD_FILE", path)

	env, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if env.DatabasePassword != "s3cr3t" {
		t.Fatalf("DatabasePassword=%q, want value from _FILE", env.DatabasePassword)
	}
}

func TestShouldLoadDotEnv(t *testing.T) {
	cases := []struct {
		appEnv, override string
		want             bool
	}{
		{"", "", true}, {"dev", "", true}, {"prod", "", false}, {"stage", "", false},
		{"prod", "true", true}, {"dev", "false", false},
		{"test", "", true}, {"production", "", false}, {"development", "", false},
	}
	for _, c := range cases {
		t.Setenv("APP_ENVIRONMENT", c.appEnv)
		t.Setenv("GOFI_DOTENV", c.override)
		if got := shouldLoadDotEnv(); got != c.want {
			t.Errorf("APP_ENVIRONMENT=%q GOFI_DOTENV=%q: got %v, want %v", c.appEnv, c.override, got, c.want)
		}
	}
}

// ---- secret references ----

func TestLoadResolvesSecretReferences(t *testing.T) {
	reset(t)
	t.Setenv("GOFI_TEST_DB_PASSWORD", "from-secret")
	t.Setenv("DATABASE_PASSWORD", "secret://env/GOFI_TEST_DB_PASSWORD")
	env, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if env.DatabasePassword != "from-secret" {
		t.Errorf("DatabasePassword=%q", env.DatabasePassword)
	}
}

func TestLoadReportsUnresolvedSecret(t *testing.T) {
	reset(t)
	t.Setenv("DATABASE_PASSWORD", "secret://env/GOFI_TEST_MISSING_SECRET")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_PASSWORD") {
		t.Errorf("err=%v, want the variable name", err)
	}
}

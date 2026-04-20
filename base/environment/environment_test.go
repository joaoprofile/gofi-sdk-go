package environment

import (
	"os"
	"path/filepath"
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

// ---- isValidCloudProvider ----

func TestIsValidCloudProvider(t *testing.T) {
	valid := []string{"aws", "gcp", "oci", "none"}
	for _, v := range valid {
		if !isValidCloudProvider(v) {
			t.Errorf("expected cloud provider %q to be valid", v)
		}
	}

	invalid := []string{"digitalocean", "linode", "vultr", ""}
	for _, v := range invalid {
		if isValidCloudProvider(v) {
			t.Errorf("expected cloud provider %q to be invalid", v)
		}
	}
}

// ---- isValidMessagingProvider ----

func TestIsValidMessagingProvider(t *testing.T) {
	valid := []string{"rabbitmq", "kafka", "sqs_sns", "aws", "gcp"}
	for _, v := range valid {
		if !isValidMessagingProvider(v) {
			t.Errorf("expected messaging provider %q to be valid", v)
		}
	}

	invalid := []string{"activemq", "nats", ""}
	for _, v := range invalid {
		if isValidMessagingProvider(v) {
			t.Errorf("expected messaging provider %q to be invalid", v)
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
	t.Setenv("CLOUD_PROVIDER", "aws")

	if err := bootstrap(); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
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
		name            string
		env             EnvironmentType
		wantDev         bool
		wantProd        bool
		wantStage       bool
		wantTest        bool
		wantCloud       bool
		wantLocal       bool
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

// ---- GetDatabaseURI ----

func TestGetDatabaseURI(t *testing.T) {
	cases := []struct {
		name   string
		env    Environment
		wantURI string
	}{
		{
			name: "postgres default",
			env: Environment{
				DatabaseHost: "localhost", DatabasePort: 5432,
				DatabaseUser: "user", DatabasePassword: "pass",
				DatabaseName: "mydb", DatabaseSSLMode: "disable",
			},
			wantURI: "host=localhost port=5432 user=user password=pass dbname=mydb sslmode=disable",
		},
		{
			name: "mysql",
			env: Environment{
				DatabaseDriver: "mysql",
				DatabaseHost:   "localhost", DatabasePort: 3306,
				DatabaseUser: "user", DatabasePassword: "pass",
				DatabaseName: "mydb",
			},
			wantURI: "user:pass@tcp(localhost:3306)/mydb",
		},
		{
			name: "sqlite",
			env: Environment{
				DatabaseDriver: "sqlite",
				DatabaseName:   "/data/app.db",
			},
			wantURI: "/data/app.db",
		},
		{
			name: "sqlite3",
			env: Environment{
				DatabaseDriver: "sqlite3",
				DatabaseName:   "/data/app.db",
			},
			wantURI: "/data/app.db",
		},
		{
			name: "pgx treated as postgres",
			env: Environment{
				DatabaseDriver: "pgx",
				DatabaseHost:   "db", DatabasePort: 5432,
				DatabaseUser: "u", DatabasePassword: "p",
				DatabaseName: "d", DatabaseSSLMode: "require",
			},
			wantURI: "host=db port=5432 user=u password=p dbname=d sslmode=require",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.env.GetDatabaseURI(); got != tc.wantURI {
				t.Errorf("GetDatabaseURI()=%q, want %q", got, tc.wantURI)
			}
		})
	}
}

// ---- typed accessors ----

func TestTypedAccessors(t *testing.T) {
	env := Environment{
		AppEnvironment:    "prod",
		CloudProvider:     "aws",
		MessagingProvider: "rabbitmq",
		CacheType:         "redis",
		LogLevel:          "debug",
	}

	if got := env.GetEnvironmentType(); got != ENV_PROD {
		t.Errorf("GetEnvironmentType()=%q, want %q", got, ENV_PROD)
	}
	if got := env.GetCloudProvider(); got != CLOUD_AWS {
		t.Errorf("GetCloudProvider()=%q, want %q", got, CLOUD_AWS)
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
		if env.IsCloudConfigured() {
			t.Error("expected IsCloudConfigured()=false for empty provider")
		}
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

	t.Run("CLOUD_NONE is not configured", func(t *testing.T) {
		env := Environment{CloudProvider: string(CLOUD_NONE)}
		if env.IsCloudConfigured() {
			t.Error("expected IsCloudConfigured()=false when provider=none")
		}
	})

	t.Run("configured", func(t *testing.T) {
		env := Environment{
			CloudProvider:     "aws",
			MessagingProvider: "kafka",
			CacheType:         "redis",
			DatabaseDriver:    "postgres",
			DatabaseHost:      "localhost",
		}
		if !env.IsCloudConfigured() {
			t.Error("expected IsCloudConfigured()=true")
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

func TestDatabaseConfig(t *testing.T) {
	env := Environment{
		DatabaseDriver:       "postgres",
		DatabaseHost:         "localhost",
		DatabasePort:         5432,
		DatabaseUser:         "user",
		DatabasePassword:     "pass",
		DatabaseName:         "mydb",
		DatabaseSSLMode:      "disable",
		DatabaseMigration:    true,
		DatabaseMaxOpenConns: 20,
		DatabaseMaxIdleConns: 5,
		DatabaseMaxLifetime:  30 * time.Second,
	}

	cfg := env.Database()
	if cfg.Driver != "postgres" {
		t.Errorf("Driver=%q, want postgres", cfg.Driver)
	}
	if cfg.MaxOpenConns != 20 {
		t.Errorf("MaxOpenConns=%d, want 20", cfg.MaxOpenConns)
	}
	if cfg.MaxLifetime != 30*time.Second {
		t.Errorf("MaxLifetime=%v, want 30s", cfg.MaxLifetime)
	}
	if cfg.URI == "" {
		t.Error("URI must not be empty")
	}
}

func TestMessagingConfig(t *testing.T) {
	env := Environment{
		MessagingProvider:       "rabbitmq",
		MessagingHost:           "localhost",
		MessagingPort:           5672,
		MessagingOCITenancyId:   "t1",
		MessagingOCIUserId:      "u1",
		MessagingOCIRegion:      "us-east-1",
		MessagingOCIFingerPrint: "fp",
	}

	cfg := env.Messaging()
	if cfg.Provider != MESSAGING_RABBITMQ {
		t.Errorf("Provider=%q, want rabbitmq", cfg.Provider)
	}
	if cfg.OCICredentials.TenancyId != "t1" {
		t.Errorf("OCICredentials.TenancyId=%q, want t1", cfg.OCICredentials.TenancyId)
	}
	if cfg.OCICredentials.FingerPrint != "fp" {
		t.Errorf("OCICredentials.FingerPrint=%q, want fp", cfg.OCICredentials.FingerPrint)
	}
}

func TestCloudConfig(t *testing.T) {
	env := Environment{
		CloudProvider:   "gcp",
		CloudRegion:     "us-central1",
		CloudDisableSSL: true,
	}

	cfg := env.Cloud()
	if cfg.Provider != CLOUD_GCP {
		t.Errorf("Provider=%q, want gcp", cfg.Provider)
	}
	if cfg.Region != "us-central1" {
		t.Errorf("Region=%q, want us-central1", cfg.Region)
	}
	if !cfg.DisableSSL {
		t.Error("expected DisableSSL=true")
	}
}

func TestCacheConfig(t *testing.T) {
	env := Environment{
		CacheType:     "redis",
		CacheURI:      "redis://localhost:6379",
		CachePassword: "secret",
	}

	cfg := env.Cache()
	if cfg.Type != REDIS_CACHE {
		t.Errorf("Type=%q, want redis", cfg.Type)
	}
	if cfg.URI != "redis://localhost:6379" {
		t.Errorf("URI=%q, want redis://localhost:6379", cfg.URI)
	}
}

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
		CloudProvider:         "invalid-cloud",     // triggers invalid-cloud warning
		MessagingProvider:     "invalid-messaging", // triggers invalid-messaging warning
		CacheType:             "invalid-cache",     // triggers invalid-cache warning
	}
	applyEnvironmentConfigurations(env)

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

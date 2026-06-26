package config

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/obs/logging"
)

func TestLogging_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_ENVIRONMENT", "dev")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")

	cfg := Logging(environment.Instance(), "billing")
	if cfg.ServiceName != "billing" {
		t.Errorf("ServiceName=%q", cfg.ServiceName)
	}
	if cfg.Environment != logging.EnvDevelopment {
		t.Errorf("Environment=%q, want dev", cfg.Environment)
	}
	if cfg.CollectorAddr != "localhost:4317" {
		t.Errorf("CollectorAddr=%q", cfg.CollectorAddr)
	}
}

func TestInitLogging(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)

	if err := InitLogging(environment.Instance(), "svc"); err != nil {
		t.Fatalf("InitLogging error: %v", err)
	}
}

func TestConfigureCache(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_NAME", "billing")
	t.Setenv("CACHE_URI", "localhost:6379")
	t.Setenv("CACHE_PASSWORD", "pw")

	// Smoke test: maps env into the sqln cache package without panicking.
	ConfigureCache(environment.Instance())
}

func TestObservability_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_NAME", "billing")
	t.Setenv("APP_ENVIRONMENT", "prod")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel:4317")

	cfg := Observability(environment.Instance())
	if cfg.ServiceName != "billing" || cfg.ServiceEnv != "prod" || cfg.CollectorAddr != "otel:4317" {
		t.Errorf("observability not mapped: %+v", cfg)
	}
}

func TestRedisBroker_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("CACHE_URI", "localhost:6379")
	t.Setenv("CACHE_PASSWORD", "pw")

	cfg := RedisBroker(environment.Instance())
	if cfg.Addr != "localhost:6379" || cfg.Password != "pw" {
		t.Errorf("redis broker not mapped: %+v", cfg)
	}
}

func TestDatabase_PoolOverrides(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_NAME", "d")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "30")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "7")
	t.Setenv("DATABASE_MAX_LIFETIME", "90s")

	cfg, err := Database(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Pool.MaxOpenConns != 30 || cfg.Pool.MaxIdleConns != 7 || cfg.Pool.MaxConnLifeTime != 90*time.Second {
		t.Errorf("pool overrides not applied: %+v", cfg.Pool)
	}
}

package core

import (
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

func TestLogging_MapsEnv(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_ENVIRONMENT", "dev")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Logging(environment.Instance(), "billing")
	if cfg.ServiceName != "billing" {
		t.Errorf("ServiceName=%q", cfg.ServiceName)
	}
	if cfg.Environment != logging.EnvDevelopment {
		t.Errorf("Environment=%q, want dev", cfg.Environment)
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

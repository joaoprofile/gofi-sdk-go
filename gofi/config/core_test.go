package config

import (
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
	"github.com/gofi-labs/gofi-sdk-go/obs/logging"
)

// The wrappers delegate to config/core, where the behavior is tested.
func TestCoreWrappers(t *testing.T) {
	logging.ResetForTesting()
	t.Cleanup(logging.ResetForTesting)
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })
	env := &environment.Environment{Timezone: "UTC"}

	if cfg := Logging(env, "svc"); cfg.ServiceName != "svc" {
		t.Errorf("Logging=%+v", cfg)
	}
	if err := InitLogging(env, "svc"); err != nil {
		t.Errorf("InitLogging: %v", err)
	}
	if Timezone(env).Name != "UTC" {
		t.Error("Timezone not mapped")
	}
	if err := SetTimezone(env); err != nil {
		t.Errorf("SetTimezone: %v", err)
	}
	ApplyTLS(env)
}

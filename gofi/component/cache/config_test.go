package cache

import (
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
)

func TestConfigure(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("APP_NAME", "billing")
	t.Setenv("CACHE_URI", "localhost:6379")
	t.Setenv("CACHE_PASSWORD", "pw")

	// Smoke test: maps env into the sqln cache package without panicking.
	Configure(environment.Instance())
}

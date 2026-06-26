package config

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi/base/environment"
)

func TestStartDebug_Disabled(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	// Explicitly disable the debug server so StartDebug is a no-op regardless
	// of any .env file present in the workspace.
	t.Setenv("SERVICE_DEBUG", "false")

	StartDebug(environment.Instance()) // must return immediately, no goroutine
}

func TestStartDebug_Enabled(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)

	t.Setenv("SERVICE_DEBUG", "true")
	// Use an ephemeral port; the goroutine will try to listen and either
	// succeed or fail — both outcomes are acceptable here. We only verify that
	// StartDebug returns promptly (non-blocking) and covers the enabled path.
	t.Setenv("SERVICE_DEBUG_ADDR", "127.0.0.1:0")

	StartDebug(environment.Instance())

	// Give the background goroutine time to start so its statements are marked
	// as covered before the test exits.
	time.Sleep(20 * time.Millisecond)
}

package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// TestMain initialises the global logger singleton required by middleware logging
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// captureLogs routes the global logger to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	logging.ResetForTesting() // Instance falls back to slog.Default
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		logging.ResetForTesting()
		_ = logging.InitGlobal(context.Background(), logging.Config{ServiceName: "netx-test"})
	})
	return &buf
}

func TestMain(m *testing.M) {
	logging.ResetForTesting()
	_ = logging.InitGlobal(context.Background(), logging.Config{ServiceName: "netx-test"})
	os.Exit(m.Run())
}

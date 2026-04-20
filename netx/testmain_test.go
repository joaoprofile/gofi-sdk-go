package netx

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
)

// TestMain initialises the global logger singleton required by middleware logging
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestMain(m *testing.M) {
	logging.ResetForTesting()
	_ = logging.InitGlobal(context.Background(), logging.Config{ServiceName: "netx-test"})
	os.Exit(m.Run())
}

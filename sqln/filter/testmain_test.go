package filter

import (
	"context"
	"os"
	"testing"

	"github.com/joaoprofile/gofi/obs/logging"
)

// TestMain initialises the global logger singleton required by filterToPredicate error paths.
// Without this, tests that exercise invalid-input branches (unknown operator, bad logical op, etc.)
// would panic because logging.Error calls logging.Instance() which panics when unset.
func TestMain(m *testing.M) {
	logging.ResetForTesting()
	_ = logging.InitGlobal(context.Background(), logging.Config{ServiceName: "filter-test"})
	os.Exit(m.Run())
}

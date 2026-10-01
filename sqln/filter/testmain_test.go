package filter

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

// TestMain initialises the global logger singleton required by filterToPredicate error paths.
// Without this, tests that exercise invalid-input branches (unknown operator, bad logical op, etc.)
// would panic because logging.Error calls logging.Instance() which panics when unset.
func TestMain(m *testing.M) {
	logging.ResetForTesting()
	_ = logging.InitGlobal(context.Background(), logging.Config{ServiceName: "filter-test"})
	os.Exit(m.Run())
}

// allowAll maps every field used in fs to itself, so tests focus on SQL
// generation rather than on the allowlist.
func allowAll(fs *Filters) Mapping {
	m := Mapping{}
	if fs == nil {
		return m
	}
	for _, f := range fs.Filters {
		if f != nil && f.Field != "" {
			m[f.Field] = Field{Column: f.Field, Sortable: true}
		}
	}
	return m
}

// build runs Build with allowAll and fails the test on error.
func build(t *testing.T, query string, fs *Filters, d FilterDialect) *QueryParam {
	t.Helper()
	qp, err := Build(query, nil, fs, allowAll(fs), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return qp
}

// buildGlobal is build with the active connection's dialect.
func buildGlobal(t *testing.T, query string, fs *Filters) *QueryParam {
	t.Helper()
	return build(t, query, fs, nil)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

package obs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The helpers live in obs/metrics; these only check the deprecated wrappers delegate.
func TestDeprecatedMetricWrappers(t *testing.T) {
	assert.NotNil(t, Meter())
	for _, err := range []error{
		func() error { _, err := NewFloat64Histogram("w.hist", "d", "ms"); return err }(),
		func() error { _, err := NewInt64Counter("w.i64", "d"); return err }(),
		func() error { _, err := NewFloat64Counter("w.f64", "d"); return err }(),
		func() error { _, err := NewInt64UpDownCounter("w.i64ud", "d"); return err }(),
		func() error { _, err := NewFloat64UpDownCounter("w.f64ud", "d"); return err }(),
		func() error { _, err := NewFloat64Gauge("w.f64g", "d", "1"); return err }(),
		func() error { _, err := NewInt64Gauge("w.i64g", "d", "1"); return err }(),
		ObserveDBStats("main", nil),
	} {
		assert.NoError(t, err)
	}
}

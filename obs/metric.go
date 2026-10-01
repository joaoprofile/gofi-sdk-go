package obs

import (
	"database/sql"

	"github.com/gofi-labs/gofi-sdk-go/obs/metrics"
	"go.opentelemetry.io/otel/metric"
)

// The metric helpers live in obs/metrics, which links no exporter; these
// wrappers keep the obs API. New code should import obs/metrics, since
// importing obs links the OTLP/gRPC exporters.

// Meter returns the gofi meter.
//
// Deprecated: use metrics.Meter.
func Meter() metric.Meter { return metrics.Meter() }

// Deprecated: use metrics.NewFloat64Histogram.
func NewFloat64Histogram(name, description, unit string) (metric.Float64Histogram, error) {
	return metrics.NewFloat64Histogram(name, description, unit)
}

// Deprecated: use metrics.NewInt64Counter.
func NewInt64Counter(name, description string) (metric.Int64Counter, error) {
	return metrics.NewInt64Counter(name, description)
}

// Deprecated: use metrics.NewFloat64Counter.
func NewFloat64Counter(name, description string) (metric.Float64Counter, error) {
	return metrics.NewFloat64Counter(name, description)
}

// Deprecated: use metrics.NewInt64UpDownCounter.
func NewInt64UpDownCounter(name, description string) (metric.Int64UpDownCounter, error) {
	return metrics.NewInt64UpDownCounter(name, description)
}

// Deprecated: use metrics.NewFloat64UpDownCounter.
func NewFloat64UpDownCounter(name, description string) (metric.Float64UpDownCounter, error) {
	return metrics.NewFloat64UpDownCounter(name, description)
}

// Deprecated: use metrics.NewFloat64Gauge.
func NewFloat64Gauge(name, description, unit string) (metric.Float64Gauge, error) {
	return metrics.NewFloat64Gauge(name, description, unit)
}

// Deprecated: use metrics.NewInt64Gauge.
func NewInt64Gauge(name, description, unit string) (metric.Int64Gauge, error) {
	return metrics.NewInt64Gauge(name, description, unit)
}

// ObserveDBStats registers the sql.DB pool gauges.
//
// Deprecated: use metrics.ObserveDBStats.
func ObserveDBStats(pool string, db *sql.DB) error { return metrics.ObserveDBStats(pool, db) }

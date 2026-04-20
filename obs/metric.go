package obs

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "gofi_otel"

func Meter() metric.Meter {
	return otel.Meter(meterName)
}

func NewFloat64Histogram(name, description, unit string) (metric.Float64Histogram, error) {
	return Meter().Float64Histogram(name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
}

func NewInt64Counter(name, description string) (metric.Int64Counter, error) {
	return Meter().Int64Counter(name,
		metric.WithDescription(description),
	)
}

func NewFloat64Counter(name, description string) (metric.Float64Counter, error) {
	return Meter().Float64Counter(name,
		metric.WithDescription(description),
	)
}

func NewInt64UpDownCounter(name, description string) (metric.Int64UpDownCounter, error) {
	return Meter().Int64UpDownCounter(name,
		metric.WithDescription(description),
	)
}

func NewFloat64UpDownCounter(name, description string) (metric.Float64UpDownCounter, error) {
	return Meter().Float64UpDownCounter(name,
		metric.WithDescription(description),
	)
}

func NewFloat64Gauge(name, description, unit string) (metric.Float64Gauge, error) {
	return Meter().Float64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
}

func NewInt64Gauge(name, description, unit string) (metric.Int64Gauge, error) {
	return Meter().Int64Gauge(name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
}

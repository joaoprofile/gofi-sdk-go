package obs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
)

func init() {
	otel.SetMeterProvider(noop.NewMeterProvider())
}

func TestMeter_ReturnsNonNil(t *testing.T) {
	m := Meter()
	assert.NotNil(t, m)
}

func TestNewFloat64Histogram(t *testing.T) {
	h, err := NewFloat64Histogram("test.histogram", "a test histogram", "ms")
	require.NoError(t, err)
	assert.NotNil(t, h)
}

func TestNewInt64Counter(t *testing.T) {
	c, err := NewInt64Counter("test.int64.counter", "a test int64 counter")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewFloat64Counter(t *testing.T) {
	c, err := NewFloat64Counter("test.float64.counter", "a test float64 counter")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewInt64UpDownCounter(t *testing.T) {
	c, err := NewInt64UpDownCounter("test.int64.updown", "a test int64 up-down counter")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewFloat64UpDownCounter(t *testing.T) {
	c, err := NewFloat64UpDownCounter("test.float64.updown", "a test float64 up-down counter")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestNewFloat64Gauge(t *testing.T) {
	g, err := NewFloat64Gauge("test.float64.gauge", "a test float64 gauge", "1")
	require.NoError(t, err)
	assert.NotNil(t, g)
}

func TestNewInt64Gauge(t *testing.T) {
	g, err := NewInt64Gauge("test.int64.gauge", "a test int64 gauge", "1")
	require.NoError(t, err)
	assert.NotNil(t, g)
}

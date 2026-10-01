package obs

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

// seriesFor records n distinct attribute sets on one counter and returns the
// number of data points the provider built from cfg exports.
func seriesFor(t *testing.T, cfg TeleConfig, n int) int {
	t.Helper()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(meterProviderOptions(cfg, resource.Empty(), reader)...)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	c, err := mp.Meter("test").Int64Counter("requests")
	require.NoError(t, err)
	for i := range n {
		c.Add(context.Background(), 1, otelmetric.WithAttributes(attribute.String("id", strconv.Itoa(i))))
	}
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return len(rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64]).DataPoints)
}

func TestMeterProviderOptions_CardinalityLimit(t *testing.T) {
	t.Setenv("OTEL_GO_X_CARDINALITY_LIMIT", "")
	// The limit includes the overflow series.
	assert.Equal(t, 5, seriesFor(t, TeleConfig{MetricCardinalityLimit: 5}, 50))
	assert.Equal(t, 2000, seriesFor(t, TeleConfig{}, 2100), "zero keeps the SDK default")
	assert.Equal(t, 2100, seriesFor(t, TeleConfig{MetricCardinalityLimit: -1}, 2100), "negative removes the cap")
}

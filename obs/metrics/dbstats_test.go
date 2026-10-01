package metrics

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(string) (sqldriver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

func (fakeConn) Prepare(string) (sqldriver.Stmt, error) { return nil, errors.New("unsupported") }
func (fakeConn) Close() error                           { return nil }
func (fakeConn) Begin() (sqldriver.Tx, error)           { return nil, errors.New("unsupported") }

func init() { sql.Register("obs-metrics-fake", fakeSQLDriver{}) }

func TestObserveDBStatsNilIsNoOp(t *testing.T) {
	assert.NoError(t, ObserveDBStats("main", nil))
}

func TestObserveDBStatsReportsPoolGauges(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	orig := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(orig) })

	db, err := sql.Open("obs-metrics-fake", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))

	require.NoError(t, ObserveDBStats("main", db))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	for _, want := range []string{"db_pool_connections", "db_pool_wait_count_total", "db_pool_wait_duration_seconds_total"} {
		assert.True(t, names[want], "missing %s", want)
	}
}

// Regression: the wait totals are cumulative, so they must be exported as
// monotonic sums (counters), not gauges.
func TestObserveDBStatsWaitTotalsAreCounters(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	orig := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(orig) })

	db, err := sql.Open("obs-metrics-fake", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, ObserveDBStats("main", db))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	kinds := map[string]any{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			kinds[m.Name] = m.Data
		}
	}
	count, ok := kinds["db_pool_wait_count_total"].(metricdata.Sum[int64])
	require.True(t, ok, "db_pool_wait_count_total is %T", kinds["db_pool_wait_count_total"])
	assert.True(t, count.IsMonotonic)
	assert.Equal(t, metricdata.CumulativeTemporality, count.Temporality)
	secs, ok := kinds["db_pool_wait_duration_seconds_total"].(metricdata.Sum[float64])
	require.True(t, ok, "db_pool_wait_duration_seconds_total is %T", kinds["db_pool_wait_duration_seconds_total"])
	assert.True(t, secs.IsMonotonic)
	_, ok = kinds["db_pool_connections"].(metricdata.Gauge[int64])
	assert.True(t, ok)
}

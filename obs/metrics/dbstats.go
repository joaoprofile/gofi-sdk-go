package metrics

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ObserveDBStats registers OpenTelemetry observable instruments that sample
// sql.DB pool stats at collection time — no hot-path cost. Reports:
//   - db_pool_connections{state=open|in_use|idle} (gauge)
//   - db_pool_wait_count_total (cumulative counter)
//   - db_pool_wait_duration_seconds_total (cumulative counter)
//
// pool labels the pool (e.g. "main"). No-op when db is nil; when telemetry is
// not initialized, Meter() returns a noop meter and the instruments are inert.
// Call once per pool (gofi's database component does it for its connections).
func ObserveDBStats(pool string, db *sql.DB) error {
	if db == nil {
		return nil
	}
	m := Meter()

	conns, err := m.Int64ObservableGauge("db_pool_connections",
		metric.WithDescription("DB pool connections by state (open/in_use/idle)"))
	if err != nil {
		return err
	}
	waitCount, err := m.Int64ObservableCounter("db_pool_wait_count_total",
		metric.WithDescription("Total number of connections waited for"))
	if err != nil {
		return err
	}
	waitSeconds, err := m.Float64ObservableCounter("db_pool_wait_duration_seconds_total",
		metric.WithDescription("Total time blocked waiting for a new connection"),
		metric.WithUnit("s"))
	if err != nil {
		return err
	}

	poolAttr := attribute.String("pool", pool)
	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := db.Stats()
		o.ObserveInt64(conns, int64(s.OpenConnections), metric.WithAttributes(poolAttr, attribute.String("state", "open")))
		o.ObserveInt64(conns, int64(s.InUse), metric.WithAttributes(poolAttr, attribute.String("state", "in_use")))
		o.ObserveInt64(conns, int64(s.Idle), metric.WithAttributes(poolAttr, attribute.String("state", "idle")))
		o.ObserveInt64(waitCount, s.WaitCount, metric.WithAttributes(poolAttr))
		o.ObserveFloat64(waitSeconds, s.WaitDuration.Seconds(), metric.WithAttributes(poolAttr))
		return nil
	}, conns, waitCount, waitSeconds)
	return err
}

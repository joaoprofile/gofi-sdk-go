package obs

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ObserveDBStats registers OpenTelemetry observable gauges that sample
// sql.DB pool stats at collection time — no hot-path cost. Reports:
//   - db_pool_connections{state=open|in_use|idle}
//   - db_pool_wait_count_total
//   - db_pool_wait_duration_seconds_total
//
// pool labels the pool (e.g. "main"). No-op when db is nil; when telemetry is
// not initialized, Meter() returns a noop meter and the gauges are inert.
// Call once per pool (the SDK does it in Build() for the managed connection).
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
	waitCount, err := m.Int64ObservableGauge("db_pool_wait_count_total",
		metric.WithDescription("Total number of connections waited for"))
	if err != nil {
		return err
	}
	waitSeconds, err := m.Float64ObservableGauge("db_pool_wait_duration_seconds_total",
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

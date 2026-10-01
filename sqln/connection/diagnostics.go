package connection

import (
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

const slowQueryThreshold = 300 * time.Millisecond

// LogQueryDuration records how long a query took.
// Queries above 300ms are logged as warning, below that as debug.
func LogQueryDuration(start time.Time, query string) {
	duration := time.Since(start)
	if duration > slowQueryThreshold {
		logging.Warn("slow query detected",
			slog.String("duration", duration.String()),
			slog.String("query", query),
		)
		return
	}
	logging.Debug("query executed",
		slog.String("duration", duration.String()),
		slog.String("query", query),
	)
}

// LogPostgresError logs database errors in structured form, with code,
// table and constraint for PostgreSQL errors. Detail and Where quote row
// values (e.g. the duplicated key), so they are logged at debug level only.
func LogPostgresError(err error) {
	if err == nil {
		return
	}
	if pgErr, ok := AsPgError(err); ok {
		logging.Error("postgres error",
			slog.String("message", pgErr.Message),
			slog.String("code", pgErr.Code),
			slog.String("severity", pgErr.Severity),
			slog.String("table", pgErr.TableName),
			slog.String("constraint", pgErr.ConstraintName),
		)
		logging.Debug("postgres error detail",
			slog.String("code", pgErr.Code),
			slog.String("detail", pgErr.Detail),
			slog.String("where", pgErr.Where),
		)
		return
	}
	logging.Error("database error", slog.String("error", Cause(err).Error()))
}

// AsPgError extracts the PostgreSQL error from err's chain.
func AsPgError(err error) (*pgconn.PgError, bool) {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr, true
	}
	return nil, false
}

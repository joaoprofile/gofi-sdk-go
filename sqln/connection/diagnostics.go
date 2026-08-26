package connection

import (
	"errors"
	"log/slog"
	"time"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/lib/pq"
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

// LogPostgresError logs database errors in structured form.
// For pq driver errors it extracts fields such as code, table and constraint.
func LogPostgresError(err error) {
	if err == nil {
		return
	}

	var pgErr *pq.Error
	if AsPQError(err, &pgErr) && pgErr != nil {
		logging.Error("postgres error",
			slog.String("message", pgErr.Message),
			slog.String("detail", pgErr.Detail),
			slog.String("where", pgErr.Where),
			slog.String("code", string(pgErr.Code)),
			slog.String("severity", pgErr.Severity),
			slog.String("table", pgErr.Table),
			slog.String("constraint", pgErr.Constraint),
		)
		return
	}

	logging.Error("database error", slog.String("error", err.Error()))
}

// AsPQError tries to extract a *pq.Error from the given error.
// Returns true and fills out when the error comes from the pq driver.
func AsPQError(err error, out **pq.Error) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		*out = pqErr
		return true
	}
	return false
}

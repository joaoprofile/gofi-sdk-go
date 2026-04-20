package connection

import (
	"errors"
	"log/slog"
	"time"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/lib/pq"
)

const slowQueryThreshold = 300 * time.Millisecond

// LogQueryDuration registra a duração de uma query.
// Queries acima de 300ms são registradas como warning; abaixo como debug.
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

// LogPostgresError loga erros de banco de forma estruturada.
// Para erros do driver pq, extrai campos como code, table e constraint.
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

// AsPQError tenta extrair um *pq.Error do erro fornecido.
// Retorna true e popula out se for um erro do driver pq.
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

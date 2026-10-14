package connection

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var queryLog atomic.Pointer[slog.Logger]

// LogQueries logs, to logger, every statement sqln sends through a Querier
// — the query manager (pages and their COUNT included), statements,
// transactions, the full-text checks and suggestions — as the database
// receives it: the SQL, its arguments, the duration and the error. nil
// stops it. The arguments may hold personal data: development only (gofi
// refuses DATABASE_LOG_QUERIES in stage and prod).
func LogQueries(logger *slog.Logger) {
	queryLog.Store(logger)
}

// Logged returns q logging its statements while LogQueries is on.
func Logged(q Querier) Querier {
	if l := queryLog.Load(); l != nil {
		if _, ok := q.(*loggingQuerier); !ok {
			return &loggingQuerier{q: q, log: l}
		}
	}
	return q
}

type loggingQuerier struct {
	q   Querier
	log *slog.Logger
}

func (l *loggingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := l.q.QueryContext(ctx, query, args...)
	l.record(ctx, "query", query, args, start, err)
	return rows, err
}

// QueryRowContext runs the query at once; its error surfaces in Scan, so it
// is not logged.
func (l *loggingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	start := time.Now()
	row := l.q.QueryRowContext(ctx, query, args...)
	l.record(ctx, "query", query, args, start, nil)
	return row
}

func (l *loggingQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	res, err := l.q.ExecContext(ctx, query, args...)
	l.record(ctx, "exec", query, args, start, err)
	return res, err
}

func (l *loggingQuerier) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	start := time.Now()
	stmt, err := l.q.PrepareContext(ctx, query)
	l.record(ctx, "prepare", query, nil, start, err)
	return stmt, err
}

func (l *loggingQuerier) record(ctx context.Context, op, query string, args []any, start time.Time, err error) {
	attrs := []slog.Attr{
		slog.String("op", op),
		slog.String("sql", strings.Join(strings.Fields(query), " ")),
		slog.Duration("duration", time.Since(start)),
	}
	if len(args) > 0 {
		attrs = append(attrs, slog.String("args", formatArgs(args)))
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelWarn
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	l.log.LogAttrs(ctx, level, "sql", attrs...)
}

// maxArgLength bounds each logged argument, in characters.
const maxArgLength = 120

// formatArgs writes the arguments as $1=… $2=…, long text cut.
func formatArgs(args []any) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		var v string
		switch x := a.(type) {
		case string:
			v = fmt.Sprintf("%q", truncate(x))
		case []byte:
			v = fmt.Sprintf("<%d bytes>", len(x))
		case nil:
			v = "NULL"
		default:
			v = truncate(fmt.Sprintf("%v", x))
		}
		fmt.Fprintf(&b, "$%d=%s", i+1, v)
	}
	return b.String()
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxArgLength {
		return s
	}
	return string([]rune(s)[:maxArgLength]) + "…"
}

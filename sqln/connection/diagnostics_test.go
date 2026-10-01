package connection

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// LogQueryDuration
// ---------------------------------------------------------------------------

func TestLogQueryDuration_FastQuery(t *testing.T) {
	// start = now → duration < 300ms → debug path
	assert.NotPanics(t, func() {
		LogQueryDuration(time.Now(), "SELECT 1")
	})
}

func TestLogQueryDuration_SlowQuery(t *testing.T) {
	// start in the past → duration > 300ms → warning path
	assert.NotPanics(t, func() {
		LogQueryDuration(time.Now().Add(-1*time.Second), "SELECT * FROM big_table")
	})
}

// ---------------------------------------------------------------------------
// AsPgError
// ---------------------------------------------------------------------------

func TestAsPgError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "unique violation", TableName: "users"}
	got, ok := AsPgError(fmt.Errorf("insert: %w", pgErr))
	assert.True(t, ok)
	assert.Same(t, pgErr, got)

	_, ok = AsPgError(errors.New("some db error"))
	assert.False(t, ok)
	_, ok = AsPgError(nil)
	assert.False(t, ok)
	assert.True(t, IsRetryable(&pgconn.PgError{Code: "40001"}), "pgx errors expose SQLState")
}

// ---------------------------------------------------------------------------
// LogPostgresError
// ---------------------------------------------------------------------------

// Regression: Detail quotes row values (PII) and was logged at error level.
func TestLogPostgresError_DetailOnlyAtDebug(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	logging.ResetForTesting()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		logging.ResetForTesting()
		_ = logging.NewLogger("connection-test")
	})

	LogPostgresError(WrapError("exec", &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: "Key (email)=(a@b.c) already exists."}))

	assert.Contains(t, buf.String(), "23505")
	assert.NotContains(t, buf.String(), "a@b.c")
}

func TestLogPostgresError(t *testing.T) {
	assert.NotPanics(t, func() {
		LogPostgresError(nil)
		LogPostgresError(&pgconn.PgError{Code: "23505", Message: "duplicate key", TableName: "products", ConstraintName: "products_pkey"})
		LogPostgresError(errors.New("connection reset by peer"))
	})
}

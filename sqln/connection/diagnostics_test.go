package connection

import (
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
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
// AsPQError
// ---------------------------------------------------------------------------

func TestAsPQError_NilError(t *testing.T) {
	var out *pq.Error
	ok := AsPQError(nil, &out)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestAsPQError_PQError(t *testing.T) {
	pqErr := &pq.Error{Code: "23505", Message: "unique violation", Table: "users"}
	var out *pq.Error
	ok := AsPQError(pqErr, &out)
	assert.True(t, ok)
	assert.Equal(t, pqErr, out)
}

func TestAsPQError_NonPQError(t *testing.T) {
	generic := errors.New("some db error")
	var out *pq.Error
	ok := AsPQError(generic, &out)
	assert.False(t, ok)
	assert.Nil(t, out)
}

func TestAsPQError_WrappedPQError(t *testing.T) {
	pqErr := &pq.Error{Code: "23503", Message: "fk violation"}
	wrapped := errors.Join(errors.New("wrapper"), pqErr)
	var out *pq.Error
	ok := AsPQError(wrapped, &out)
	assert.True(t, ok)
	assert.Equal(t, pqErr, out)
}

// ---------------------------------------------------------------------------
// LogPostgresError
// ---------------------------------------------------------------------------

func TestLogPostgresError_NilError(t *testing.T) {
	assert.NotPanics(t, func() {
		LogPostgresError(nil)
	})
}

func TestLogPostgresError_PQError(t *testing.T) {
	pqErr := &pq.Error{
		Code:       "23505",
		Message:    "duplicate key",
		Detail:     "key exists",
		Severity:   "ERROR",
		Table:      "products",
		Constraint: "products_pkey",
	}
	assert.NotPanics(t, func() {
		LogPostgresError(pqErr)
	})
}

func TestLogPostgresError_GenericError(t *testing.T) {
	assert.NotPanics(t, func() {
		LogPostgresError(errors.New("connection reset by peer"))
	})
}

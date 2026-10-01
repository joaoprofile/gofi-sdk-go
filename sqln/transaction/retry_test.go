package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
)

type stateErr string

func (e stateErr) Error() string    { return "sqlstate " + string(e) }
func (e stateErr) SQLState() string { return string(e) }

func TestIsRetryable(t *testing.T) {
	assert.True(t, connection.IsRetryable(stateErr("40001")))
	assert.True(t, connection.IsRetryable(fmt.Errorf("wrapped: %w", stateErr("40P01"))))
	assert.False(t, connection.IsRetryable(stateErr("23505")))
	assert.False(t, connection.IsRetryable(errors.New("plain")))
}

func TestRetry_RepeatsOnSerializationFailure(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{MaxRetries: 3}}
	calls := 0
	err := tr.executeWithRetry(context.Background(), db, func(context.Context) error {
		calls++
		if calls < 3 {
			return stateErr("40001")
		}
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetry_StopsAtMaxRetries(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{MaxRetries: 2}}
	calls := 0
	err := tr.executeWithRetry(context.Background(), db, func(context.Context) error {
		calls++
		return stateErr("40P01")
	})
	assert.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetry_OffByDefaultAndForOtherErrors(t *testing.T) {
	db := rawDB(t, "ok")
	calls := 0
	fn := func(context.Context) error { calls++; return stateErr("40001") }
	_ = (&transaction{}).executeWithRetry(context.Background(), db, fn)
	assert.Equal(t, 1, calls, "MaxRetries=0 keeps the old behaviour")

	calls = 0
	_ = (&transaction{opts: Options{MaxRetries: 3}}).executeWithRetry(context.Background(), db,
		func(context.Context) error { calls++; return errors.New("business") })
	assert.Equal(t, 1, calls)
}

func TestRetry_NestedDoesNotRetry(t *testing.T) {
	db := rawDB(t, "ok")
	tx, _ := db.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	ctx := context.WithValue(context.Background(), connection.SqlTxContextKey, tx)
	calls := 0
	_ = (&transaction{opts: Options{MaxRetries: 3}}).executeWithRetry(ctx, db,
		func(context.Context) error { calls++; return stateErr("40001") })
	assert.Equal(t, 1, calls, "the outermost transaction owns retries")
}

func TestRetry_StopsWhenContextEnds(t *testing.T) {
	db := rawDB(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = (&transaction{opts: Options{MaxRetries: 1000}}).executeWithRetry(ctx, db,
		func(context.Context) error { return stateErr("40001") })
	assert.Less(t, time.Since(start), time.Second)
}

func TestNew_ReadOnlyOption(t *testing.T) {
	tr := New(Options{Isolation: sql.LevelSerializable, ReadOnly: true}).(*transaction)
	assert.True(t, tr.opts.ReadOnly)
}

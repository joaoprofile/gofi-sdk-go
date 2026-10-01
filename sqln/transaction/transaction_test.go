package transaction

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewTransaction

func TestNewTransaction_DefaultIsolation(t *testing.T) {
	tx := NewTransaction()
	assert.NotNil(t, tx)
	assert.Equal(t, sql.LevelDefault, tx.(*transaction).opts.Isolation)
}

func TestNewTransaction_SingleIsolation(t *testing.T) {
	tx := NewTransaction(sql.LevelSerializable)
	assert.NotNil(t, tx)
	assert.Equal(t, sql.LevelSerializable, tx.(*transaction).opts.Isolation)
}

func TestNewTransaction_MultipleIsolations_UsesFirst(t *testing.T) {
	// When more than one level is passed the first is used and a warning is logged.
	tx := NewTransaction(sql.LevelSerializable, sql.LevelReadCommitted)
	assert.NotNil(t, tx)
	assert.Equal(t, sql.LevelSerializable, tx.(*transaction).opts.Isolation)
}

// Execute — success path (uses global connection)

func TestExecute_Success_CommitsTransaction(t *testing.T) {
	setupGlobal(t, "ok")

	tx := NewTransaction()
	called := false
	err := tx.Execute(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestExecute_ContextContainsSqlTx(t *testing.T) {
	setupGlobal(t, "ok")

	tx := NewTransaction()
	var capturedTx *sql.Tx

	_ = tx.Execute(context.Background(), func(ctx context.Context) error {
		capturedTx, _ = ctx.Value(connection.SqlTxContextKey).(*sql.Tx)
		return nil
	})

	assert.NotNil(t, capturedTx)
}

// Execute — fn returns error → rollback

func TestExecute_FnError_Rollbacks(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	fnErr := errors.New("business error")
	err := tr.executeTransaction(context.Background(), db, func(_ context.Context) error {
		return fnErr
	})

	assert.Equal(t, fnErr, err)
}

func TestExecute_FnError_RollbackFails_WrapsErrors(t *testing.T) {
	db := rawDB(t, "fail-rollback")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	fnErr := errors.New("fn error")
	err := tr.executeTransaction(context.Background(), db, func(_ context.Context) error {
		return fnErr
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fn error")
	assert.Contains(t, err.Error(), "rollback failed")
	assert.ErrorIs(t, err, fnErr, "callers must still match the domain error")
}

func TestExecute_BeginCanceled_PreservesContextError(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tr.executeTransaction(ctx, db, func(context.Context) error { return nil })
	assert.ErrorIs(t, err, context.Canceled)
}

// Execute — begin fails

func TestExecute_BeginFails_ReturnsError(t *testing.T) {
	db := rawDB(t, "fail-begin")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	err := tr.executeTransaction(context.Background(), db, func(_ context.Context) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "begin failed")
}

// Execute — commit fails

func TestExecute_CommitFails_ReturnsError(t *testing.T) {
	db := rawDB(t, "fail-commit")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	err := tr.executeTransaction(context.Background(), db, func(_ context.Context) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "commit failed")
}

// Execute — panic inside fn → rollback + re-panic

func TestExecute_PanicInsideFn_RollbacksAndRepanics(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	assert.Panics(t, func() {
		_ = tr.executeTransaction(context.Background(), db, func(_ context.Context) error {
			panic("something went very wrong")
		})
	})
}

// Execute — nested transaction (fn receives ctx with *sql.Tx injected)

func TestExecute_NestedContext_TxPropagated(t *testing.T) {
	db := rawDB(t, "ok")
	tr := &transaction{opts: Options{Isolation: sql.LevelDefault}}

	var outerTx, innerTx *sql.Tx

	_ = tr.executeTransaction(context.Background(), db, func(ctx context.Context) error {
		outerTx, _ = ctx.Value(connection.SqlTxContextKey).(*sql.Tx)

		// Simulate a downstream call that also reads the key.
		innerTx, _ = ctx.Value(connection.SqlTxContextKey).(*sql.Tx)
		return nil
	})

	assert.NotNil(t, outerTx)
	assert.Equal(t, outerTx, innerTx, "same *sql.Tx must be visible through ctx")
}

func TestExecute_NestedExecute_JoinsOuterTransaction(t *testing.T) {
	setupGlobal(t, "ok")
	beginCount.Store(0)

	var outerTx, innerTx *sql.Tx

	err := NewTransaction().Execute(context.Background(), func(ctx context.Context) error {
		outerTx, _ = ctx.Value(connection.SqlTxContextKey).(*sql.Tx)

		return NewTransaction().Execute(ctx, func(innerCtx context.Context) error {
			innerTx, _ = innerCtx.Value(connection.SqlTxContextKey).(*sql.Tx)
			return nil
		})
	})

	require.NoError(t, err)
	require.NotNil(t, outerTx)
	assert.Same(t, outerTx, innerTx, "Execute aninhado deve reusar a *sql.Tx externa")
	assert.Equal(t, int32(1), beginCount.Load(), "apenas uma transação pode ser aberta no driver")
}

func TestExecute_NestedExecute_InnerErrorRollsBackOuter(t *testing.T) {
	setupGlobal(t, "ok")
	beginCount.Store(0)

	innerErr := errors.New("inner failed")
	afterInner := false

	err := NewTransaction().Execute(context.Background(), func(ctx context.Context) error {
		if inner := NewTransaction().Execute(ctx, func(context.Context) error {
			return innerErr
		}); inner != nil {
			return inner
		}
		afterInner = true
		return nil
	})

	require.ErrorIs(t, err, innerErr)
	assert.False(t, afterInner)
	assert.Equal(t, int32(1), beginCount.Load())
}

package transaction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln/connection"
)

const (
	msgIsolationIgnored = "transaction isolation only uses the first parameter, others are ignored"
	errExecute          = "error when executing transaction error: %w"
	errRollback         = "error when executing transaction rollback: %v, original error: %w"
	errCommit           = "could not commit transaction: %w"
	errStart            = "could not start database transaction: %v"
)

type Transaction interface {
	Execute(ctx context.Context, fn func(ctx context.Context) error) error
}

type transaction struct {
	isolationLevel sql.IsolationLevel
}

func NewTransaction(isolation ...sql.IsolationLevel) Transaction {
	isolationLevel := sql.LevelDefault
	if len(isolation) == 1 {
		isolationLevel = isolation[0]
	} else if len(isolation) > 1 {
		isolationLevel = isolation[0]
		logging.Warn(msgIsolationIgnored)
	}
	return &transaction{isolationLevel: isolationLevel}
}

func (t *transaction) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.executeTransaction(ctx, connection.MustDB(), fn)
}

func (t *transaction) executeTransaction(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	if outer, ok := ctx.Value(connection.SqlTxContextKey).(*sql.Tx); ok && outer != nil {
		return fn(ctx)
	}

	tx, err := t.beginTransaction(ctx, db)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	ctx = context.WithValue(ctx, connection.SqlTxContextKey, tx)

	if err := fn(ctx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			logging.Error(errRollback, err, rbErr)
			return fmt.Errorf(errRollback, err, rbErr)
		}
		logging.Error(errExecute, slog.Any("error", err))
		return err
	}

	if err := tx.Commit(); err != nil {
		logging.Error(errCommit, err)
		return fmt.Errorf(errCommit, err)
	}

	return nil
}

func (t *transaction) beginTransaction(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: t.isolationLevel})
	if err != nil {
		logging.Error(errStart, err)
		return nil, fmt.Errorf(errStart, err)
	}
	return tx, nil
}

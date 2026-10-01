package transaction

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
)

const (
	msgIsolationIgnored = "transaction isolation only uses the first parameter, others are ignored"
	errRollback         = "error when executing transaction rollback: %w, original error: %w"
	errCommit           = "could not commit transaction: %w"
	errStart            = "could not start database transaction: %w"
)

type Transaction interface {
	Execute(ctx context.Context, fn func(ctx context.Context) error) error
}

// Options configures a transaction.
type Options struct {
	Isolation sql.IsolationLevel
	ReadOnly  bool
	// Connection selects the database; nil uses the global connection.
	Connection *connection.Connection
	// MaxRetries re-runs fn after serialization failures and deadlocks
	// (connection.IsRetryable). fn must be safe to repeat: no side effects
	// outside the transaction.
	MaxRetries int
}

type transaction struct {
	opts Options
}

// New returns a Transaction with opts.
func New(opts Options) Transaction {
	return &transaction{opts: opts}
}

// NewTransaction returns a Transaction with the given isolation level.
func NewTransaction(isolation ...sql.IsolationLevel) Transaction {
	opts := Options{Isolation: sql.LevelDefault}
	if len(isolation) > 0 {
		opts.Isolation = isolation[0]
	}
	if len(isolation) > 1 {
		logging.Warn(msgIsolationIgnored)
	}
	return New(opts)
}

func (t *transaction) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	db := connection.MustDB
	if t.opts.Connection != nil {
		db = t.opts.Connection.DB
	}
	return t.executeWithRetry(ctx, db(), fn)
}

const (
	retryBaseDelay = 20 * time.Millisecond
	retryMaxDelay  = time.Second
)

// executeWithRetry repeats the whole transaction on retryable conflicts, with
// jittered exponential backoff. Nested calls never retry: the outermost does.
func (t *transaction) executeWithRetry(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	for attempt := 0; ; attempt++ {
		err := t.executeTransaction(ctx, db, fn)
		if err == nil || attempt >= t.opts.MaxRetries || !connection.IsRetryable(err) {
			return err
		}
		if _, nested := connection.TxFor(ctx, db); nested {
			return err
		}
		delay := min(retryBaseDelay<<attempt, retryMaxDelay)
		delay = delay/2 + rand.N(delay/2+1) // #nosec G404 -- backoff jitter, not a secret
		logging.Warn("transaction conflict, retrying", slog.Int("attempt", attempt+1), slog.Duration("delay", delay), slog.Any("error", err))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

func (t *transaction) executeTransaction(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	if _, ok := connection.TxFor(ctx, db); ok {
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

	ctx = connection.WithTx(ctx, db, tx)

	if err := fn(ctx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			wrapped := fmt.Errorf(errRollback, rbErr, err)
			logging.Error("transaction rollback failed", slog.Any("error", wrapped))
			return wrapped
		}
		logging.Error("transaction rolled back", slog.Any("error", err))
		return err
	}

	if err := tx.Commit(); err != nil {
		wrapped := fmt.Errorf(errCommit, err)
		logging.Error("transaction commit failed", slog.Any("error", wrapped))
		return wrapped
	}

	return nil
}

func (t *transaction) beginTransaction(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: t.opts.Isolation, ReadOnly: t.opts.ReadOnly})
	if err != nil {
		wrapped := fmt.Errorf(errStart, err)
		logging.Error("transaction begin failed", slog.Any("error", wrapped))
		return nil, wrapped
	}
	return tx, nil
}

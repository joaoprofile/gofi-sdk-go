package connection

import (
	"context"
	"database/sql"
)

// txContextKey is unexported to prevent key collisions with other packages.
type txContextKey string

// SqlTxContextKey is the context key used to propagate *sql.Tx within a request.
// transaction.Execute stores the active transaction under this key; Statement
// and Query read it to participate in the same transaction automatically.
// With several databases use TxFor, which also knows which pool owns it.
const SqlTxContextKey txContextKey = "sqlTxContext"

// txBindingsKey holds the transactions of the context by owning pool, so a
// statement on one database never joins another database's transaction.
type txBindingsKey struct{}

type txBinding struct {
	db *sql.DB
	tx *sql.Tx
}

// WithTx returns ctx carrying tx opened on db. Outer transactions on other
// pools stay available through TxFor.
func WithTx(ctx context.Context, db *sql.DB, tx *sql.Tx) context.Context {
	prev, _ := ctx.Value(txBindingsKey{}).([]txBinding)
	bindings := append(append(make([]txBinding, 0, len(prev)+1), prev...), txBinding{db: db, tx: tx})
	ctx = context.WithValue(ctx, SqlTxContextKey, tx)
	return context.WithValue(ctx, txBindingsKey{}, bindings)
}

// TxFor returns the transaction ctx carries for db. A transaction stored
// directly under SqlTxContextKey, without WithTx, is assumed to be db's.
func TxFor(ctx context.Context, db *sql.DB) (*sql.Tx, bool) {
	bindings, _ := ctx.Value(txBindingsKey{}).([]txBinding)
	if len(bindings) == 0 {
		return TxFrom(ctx)
	}
	for i := len(bindings) - 1; i >= 0; i-- {
		if bindings[i].db == db {
			return bindings[i].tx, true
		}
	}
	return nil, false
}

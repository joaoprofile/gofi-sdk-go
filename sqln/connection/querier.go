package connection

import (
	"context"
	"database/sql"
)

// Querier is the common surface of *sql.DB, *sql.Tx and *sql.Conn, so
// repositories run the same code inside and outside a transaction.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

var (
	_ Querier = (*sql.DB)(nil)
	_ Querier = (*sql.Tx)(nil)
	_ Querier = (*sql.Conn)(nil)
)

// TxFrom returns the transaction carried by ctx (see transaction.Execute).
func TxFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(SqlTxContextKey).(*sql.Tx)
	return tx, ok && tx != nil
}

// QuerierFrom returns db's transaction carried by ctx, or db.
func QuerierFrom(ctx context.Context, db *sql.DB) Querier {
	if tx, ok := TxFor(ctx, db); ok {
		return tx
	}
	return db
}

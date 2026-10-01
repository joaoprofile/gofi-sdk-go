package transaction

import (
	"context"
	"database/sql"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func conn(t *testing.T, dsn string) *connection.Connection {
	t.Helper()
	c, err := connection.NewConnection(connection.Config{Driver: connection.DriverName(testDriver), DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	return c
}

// A transaction on one database is never used for another database.
func TestTransaction_BoundToItsDatabase(t *testing.T) {
	initDriver()
	orders, billing := conn(t, "ok"), conn(t, "ok")

	err := New(Options{Connection: orders}).Execute(context.Background(), func(ctx context.Context) error {
		ordersTx, ok := connection.TxFor(ctx, orders.DB())
		require.True(t, ok)
		assert.Same(t, ordersTx, connection.QuerierFrom(ctx, orders.DB()).(*sql.Tx))
		assert.Same(t, billing.DB(), connection.QuerierFrom(ctx, billing.DB()), "other database runs outside the transaction")

		// A nested transaction on the second database opens its own.
		return New(Options{Connection: billing}).Execute(ctx, func(ctx context.Context) error {
			billingTx, ok := connection.TxFor(ctx, billing.DB())
			require.True(t, ok)
			assert.NotSame(t, ordersTx, billingTx)
			stillOrders, ok := connection.TxFor(ctx, orders.DB())
			require.True(t, ok)
			assert.Same(t, ordersTx, stillOrders, "outer transaction stays reachable")
			return nil
		})
	})
	require.NoError(t, err)
}

// Code that stores a transaction under SqlTxContextKey by hand keeps working.
func TestTxFor_LegacyContextValue(t *testing.T) {
	db := rawDB(t, "ok")
	tx, err := db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := context.WithValue(context.Background(), connection.SqlTxContextKey, tx)
	got, ok := connection.TxFor(ctx, db)
	assert.True(t, ok)
	assert.Same(t, tx, got)
}

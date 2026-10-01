package sqln

import (
	"context"
	"database/sql"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAll_StreamsRows(t *testing.T) {
	setupGlobal(t, "multi-rows")
	var names []string
	for item, err := range Find[mappedItem](context.Background(), "SELECT id, name FROM t").All() {
		require.NoError(t, err)
		names = append(names, item.Name)
	}
	assert.Equal(t, []string{"a", "b", "c"}, names)
}

func TestAll_EarlyBreakReleasesTheConnection(t *testing.T) {
	db := setupGlobal(t, "multi-rows")
	db.SetMaxOpenConns(1)
	for range 3 {
		for _, err := range Find[mappedItem](context.Background(), "SELECT id, name FROM t").All() {
			require.NoError(t, err)
			break
		}
	}
	assert.Equal(t, 0, db.Stats().InUse, "rows must be closed after an early break")
}

func TestAll_EmptyQueryYieldsError(t *testing.T) {
	setupGlobal(t, "multi-rows")
	var errs int
	for _, err := range Find[mappedItem](context.Background(), "").All() {
		assert.Error(t, err)
		errs++
	}
	assert.Equal(t, 1, errs)
}

func TestQuerierFrom_PrefersTransaction(t *testing.T) {
	db := openDB(t, "ok")
	assert.Same(t, db, connection.QuerierFrom(context.Background(), db))

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := context.WithValue(context.Background(), connection.SqlTxContextKey, tx)
	got, ok := connection.TxFrom(ctx)
	assert.True(t, ok)
	assert.Same(t, tx, got)
	assert.Same(t, tx, connection.QuerierFrom(ctx, db).(*sql.Tx))
}

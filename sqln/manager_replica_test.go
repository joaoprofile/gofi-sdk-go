package sqln

import (
	"context"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replicaConn(t *testing.T) *connection.Connection {
	t.Helper()
	initDriver()
	connection.ResetGlobalForTest()
	conn, err := connection.NewConnection(connection.Config{
		Driver:  connection.DriverName(testDriverName),
		DSN:     "ok",         // primary: no rows
		ReadDSN: "multi-rows", // replica: three rows
		Pool:    connection.PoolConfig{MaxIdleTime: time.Minute},
	})
	require.NoError(t, err)
	connection.SetGlobal(conn)
	t.Cleanup(func() { _ = conn.Close(); connection.ResetGlobalForTest() })
	return conn
}

func TestReplica_ReadsGoToReplica(t *testing.T) {
	conn := replicaConn(t)
	assert.NotSame(t, conn.DB(), conn.ReadDB())

	list, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").List()
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

func TestReplica_TransactionReadsUsePrimary(t *testing.T) {
	replicaConn(t)
	err := transaction.NewTransaction().Execute(context.Background(), func(ctx context.Context) error {
		list, err := Find[mappedItem](ctx, "SELECT id, name FROM t").List()
		require.NoError(t, err)
		assert.Empty(t, list, "reads inside a transaction must see its writes")
		return nil
	})
	require.NoError(t, err)
}

func TestReplica_ReadDBFallsBackToPrimary(t *testing.T) {
	db := setupGlobal(t, "ok")
	conn, _ := connection.Global()
	assert.Same(t, db, conn.ReadDB())
}

func TestReplica_FromPrimaryReadsPrimary(t *testing.T) {
	replicaConn(t)

	list, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").FromPrimary().List()
	require.NoError(t, err)
	assert.Empty(t, list, "FromPrimary must bypass the replica (primary has no rows, replica has three)")

	unique, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").FromPrimary().UniqueResult()
	require.NoError(t, err)
	assert.Nil(t, unique)
}

// FromPrimary exists to confirm a verdict against fresh data; a cache entry
// shared with replica reads (same database ID) would defeat it.
func TestReplica_FromPrimaryBypassesCache(t *testing.T) {
	conn := replicaConn(t)
	c := withMiniredisCache[mappedItem](t, "from-primary")

	q := Find[mappedItem](context.Background(), "SELECT id, name FROM t").WithCache(c)
	_, ok := q.cacheKey(conn.ReadDB(), "list")
	assert.True(t, ok, "a replica read is cacheable")

	_, ok = q.FromPrimary().cacheKey(conn.DB(), "list")
	assert.False(t, ok, "a primary read must not touch the cache")
}

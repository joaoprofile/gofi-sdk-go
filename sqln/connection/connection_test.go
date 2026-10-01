package connection

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnection_DriverNotRegistered(t *testing.T) {
	conn, err := NewConnection(Config{Driver: "unknown-driver", DSN: "any"})
	assert.Nil(t, conn)
	assert.ErrorContains(t, err, ErrDriverNotRegistered)
}

func TestNewConnection_OpenFails(t *testing.T) {
	registerFakeDriverOnce()

	conn, err := NewConnection(Config{
		Driver: DriverName(testDriverDSN),
		DSN:    "fail-open",
	})
	assert.Nil(t, conn)
	assert.ErrorContains(t, err, ErrPingFailed)
}

func TestNewConnection_PingFails(t *testing.T) {
	registerFakeDriverOnce()

	conn, err := NewConnection(Config{
		Driver: DriverName(testDriverDSN),
		DSN:    "fail-ping",
	})
	assert.Nil(t, conn)
	assert.ErrorContains(t, err, ErrPingFailed)
}

func TestNewConnection_Success(t *testing.T) {
	registerFakeDriverOnce()

	conn, err := NewConnection(Config{
		Driver: DriverName(testDriverDSN),
		DSN:    "ok",
	})
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.NotNil(t, conn.DB())
	assert.Equal(t, DefaultQueryTimeout, conn.QueryTimeout())
	assert.Equal(t, DefaultPoolConfig().MaxOpenConns, conn.DB().Stats().MaxOpenConnections)

	_ = conn.Close()
}

func TestNewConnection_QueryTimeout(t *testing.T) {
	registerFakeDriverOnce()

	conn, err := NewConnection(Config{Driver: DriverName(testDriverDSN), DSN: "ok", QueryTimeout: time.Second})
	require.NoError(t, err)
	defer conn.Close()
	assert.Equal(t, time.Second, conn.QueryTimeout())
	ctx, cancel := conn.WithQueryTimeout(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(time.Second), deadline, 500*time.Millisecond)
}

func TestNewConnection_WithPoolConfig(t *testing.T) {
	registerFakeDriverOnce()

	pool := DefaultPoolConfig()
	pool.MaxOpenConns = 5
	pool.MaxIdleConns = 2

	conn, err := NewConnection(Config{
		Driver: DriverName(testDriverDSN),
		DSN:    "ok",
		Pool:   pool,
	})
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}

func TestNewRaw(t *testing.T) {
	db := mustOpenTestDB("ok")
	drv := fakeConnDriver{}

	conn := NewRaw(db, drv)
	assert.NotNil(t, conn)
	assert.Equal(t, db, conn.DB())
}

func TestConnection_DB(t *testing.T) {
	conn := newTestConnection("ok")
	assert.NotNil(t, conn.DB())
}

func TestConnection_Close(t *testing.T) {
	conn := newTestConnection("ok")
	err := conn.Close()
	assert.NoError(t, err)
}

func TestConnection_Dialect(t *testing.T) {
	conn := newTestConnection("ok")
	dialect := conn.Dialect()
	assert.NotNil(t, dialect)
	assert.Equal(t, "?", dialect.Param(1))
}

// ID keys the query cache: one database, one ID; never the DSN (or its password) in clear.
func TestConnection_ID(t *testing.T) {
	registerFakeDriverOnce()
	open := func(dsn string) *Connection {
		c, err := NewConnection(Config{Driver: DriverName(testDriverDSN), DSN: dsn})
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	a, a2, b := open("ok"), open("ok"), open("ok?password=s3cret")
	assert.Equal(t, a.ID(), a2.ID())
	assert.NotEqual(t, a.ID(), b.ID())
	assert.NotContains(t, b.ID(), "s3cret")

	db := mustOpenTestDB("ok")
	r1, r2 := NewRaw(db, fakeConnDriver{}), NewRaw(db, fakeConnDriver{})
	assert.NotEmpty(t, r1.ID())
	assert.NotEqual(t, r1.ID(), r2.ID())
}

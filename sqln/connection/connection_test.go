package connection

import (
	"testing"

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

	_ = conn.Close()
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

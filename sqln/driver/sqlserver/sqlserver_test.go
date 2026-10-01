package sqlserver

import (
	"errors"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	sqln_driver "github.com/gofi-labs/gofi-sdk-go/sqln/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriver_Name_ReturnsSQLServer(t *testing.T) {
	assert.Equal(t, connection.DriverSQLServer, Driver{}.Name())
}

func TestDriver_ParseError_NilInput_ReturnsNil(t *testing.T) {
	assert.Nil(t, Driver{}.ParseError(nil))
}

func TestDriver_ParseError_HidesMessageKeepsCause(t *testing.T) {
	err := errors.New("sqlserver error")
	got := Driver{}.ParseError(err)
	assert.ErrorIs(t, got, err)
	assert.NotContains(t, got.Error(), "sqlserver error")
}

func TestDriver_Dialect_ImplementsDialectInterface(t *testing.T) {
	var _ sqln_driver.Dialect = Driver{}.Dialect()
	require.NotNil(t, Driver{}.Dialect())
}

func TestDriver_Dialect_ReturnsSQLServerDialect(t *testing.T) {
	_, ok := Driver{}.Dialect().(SQLServerDialect)
	assert.True(t, ok)
}

func TestDriver_RegisteredInConnectionRegistry(t *testing.T) {
	d, ok := connection.GetDriver(connection.DriverSQLServer)
	require.True(t, ok, "sqlserver driver should be auto-registered via init()")
	assert.Equal(t, connection.DriverSQLServer, d.Name())
}

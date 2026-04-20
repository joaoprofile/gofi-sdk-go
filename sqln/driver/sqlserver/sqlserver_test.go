package sqlserver

import (
	"errors"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriver_Name_ReturnsSQLServer(t *testing.T) {
	assert.Equal(t, connection.DriverSQLServer, Driver{}.Name())
}

func TestDriver_ParseError_NilInput_ReturnsNil(t *testing.T) {
	assert.Nil(t, Driver{}.ParseError(nil))
}

func TestDriver_ParseError_NonNilInput_ReturnsSameError(t *testing.T) {
	err := errors.New("sqlserver error")
	assert.Equal(t, err, Driver{}.ParseError(err))
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

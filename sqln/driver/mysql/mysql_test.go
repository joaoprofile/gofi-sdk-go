package mysql

import (
	"errors"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriver_Name_ReturnsMySQL(t *testing.T) {
	assert.Equal(t, connection.DriverMySQL, Driver{}.Name())
}

func TestDriver_ParseError_NilInput_ReturnsNil(t *testing.T) {
	assert.Nil(t, Driver{}.ParseError(nil))
}

func TestDriver_ParseError_NonNilInput_ReturnsSameError(t *testing.T) {
	err := errors.New("mysql error")
	assert.Equal(t, err, Driver{}.ParseError(err))
}

func TestDriver_Dialect_ImplementsDialectInterface(t *testing.T) {
	var _ sqln_driver.Dialect = Driver{}.Dialect()
	require.NotNil(t, Driver{}.Dialect())
}

func TestDriver_Dialect_ReturnsMySQLDialect(t *testing.T) {
	_, ok := Driver{}.Dialect().(MySQLDialect)
	assert.True(t, ok)
}

func TestDriver_RegisteredInConnectionRegistry(t *testing.T) {
	d, ok := connection.GetDriver(connection.DriverMySQL)
	require.True(t, ok, "mysql driver should be auto-registered via init()")
	assert.Equal(t, connection.DriverMySQL, d.Name())
}

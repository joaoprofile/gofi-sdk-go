package postgres

import (
	"errors"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriver_Name_ReturnsPostgres(t *testing.T) {
	assert.Equal(t, connection.DriverPostgres, Driver{}.Name())
}

func TestDriver_ParseError_NilInput_ReturnsNil(t *testing.T) {
	assert.Nil(t, Driver{}.ParseError(nil))
}

func TestDriver_ParseError_NonNilInput_ReturnsSameError(t *testing.T) {
	err := errors.New("some db error")
	assert.Equal(t, err, Driver{}.ParseError(err))
}

func TestDriver_Dialect_ImplementsDialectInterface(t *testing.T) {
	var _ sqln_driver.Dialect = Driver{}.Dialect()
	require.NotNil(t, Driver{}.Dialect())
}

func TestDriver_Dialect_ReturnsPostgresDialect(t *testing.T) {
	_, ok := Driver{}.Dialect().(PostgresDialect)
	assert.True(t, ok)
}

func TestDriver_RegisteredInConnectionRegistry(t *testing.T) {
	d, ok := connection.GetDriver(connection.DriverPostgres)
	require.True(t, ok, "postgres driver should be auto-registered via init()")
	assert.Equal(t, connection.DriverPostgres, d.Name())
}

// Open — sql.Open is lazy: it validates driver registration without connecting.
// A real server is not required; errors only arise when the DB is actually used.
func TestDriver_Open_ReturnsSQLDB(t *testing.T) {
	db, err := Driver{}.Open(connection.Config{DSN: "host=127.0.0.1 port=1 dbname=test sslmode=disable"})
	require.NoError(t, err)
	require.NotNil(t, db)
	db.Close()
}

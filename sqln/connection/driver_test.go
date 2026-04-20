package connection

import (
	"database/sql"
	"testing"

	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
)

func TestGetDriver_NotRegistered(t *testing.T) {
	_, ok := GetDriver("driver-that-does-not-exist")
	assert.False(t, ok)
}

func TestGetDriver_Registered(t *testing.T) {
	registerFakeDriverOnce()

	got, ok := GetDriver(DriverName(testDriverDSN))
	assert.True(t, ok)
	assert.NotNil(t, got)
	assert.Equal(t, DriverName(testDriverDSN), got.Name())
}

func TestRegisterDriver_OverwritesSameName(t *testing.T) {
	// Registrar um driver com nome diferente para cobrir RegisterDriver
	name := DriverName("overwrite-test")
	drv1 := overwriteDriver{name: name, id: 1}
	drv2 := overwriteDriver{name: name, id: 2}

	RegisterDriver(drv1)
	RegisterDriver(drv2)

	got, ok := GetDriver(name)
	assert.True(t, ok)
	assert.Equal(t, 2, got.(overwriteDriver).id)
}

func TestGetDriver_KnownDriverNames(t *testing.T) {
	assert.Equal(t, DriverName("postgres"), DriverPostgres)
	assert.Equal(t, DriverName("mysql"), DriverMySQL)
	assert.Equal(t, DriverName("oracle"), DriverOracle)
	assert.Equal(t, DriverName("sqlserver"), DriverSQLServer)
}

// overwriteDriver é um Driver mínimo para testar sobrescrita no registry.
type overwriteDriver struct {
	name DriverName
	id   int
}

func (d overwriteDriver) Name() DriverName               { return d.name }
func (d overwriteDriver) Open(_ Config) (*sql.DB, error) { return nil, nil }
func (d overwriteDriver) ParseError(err error) error     { return err }
func (d overwriteDriver) Dialect() sqln_driver.Dialect   { return fakeDialect{} }

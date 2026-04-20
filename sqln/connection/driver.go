package connection

import (
	"database/sql"

	"github.com/joaoprofile/gofi/sqln/driver"
)

type DriverName string

const (
	DriverPostgres  DriverName = "postgres"
	DriverMySQL     DriverName = "mysql"
	DriverOracle    DriverName = "oracle"
	DriverSQLServer DriverName = "sqlserver"
)

type Driver interface {
	Name() DriverName
	Open(cfg Config) (*sql.DB, error)
	ParseError(err error) error
	Dialect() driver.Dialect
}

var drivers = map[DriverName]Driver{}

func RegisterDriver(d Driver) {
	drivers[d.Name()] = d
}

func getDriver(name DriverName) (Driver, bool) {
	d, ok := drivers[name]
	return d, ok
}

func GetDriver(name DriverName) (Driver, bool) {
	return getDriver(name)
}

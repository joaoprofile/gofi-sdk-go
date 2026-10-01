package connection

import (
	"database/sql"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
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
	// DSN assembles the driver-specific connection string from s. Each driver
	// owns its own format (key-value for postgres, URL for sqlserver, …) so new
	// databases can be added without a central switch. Every value is escaped;
	// use BuildDSN to also reject invalid settings.
	DSN(s Settings) string
	Open(cfg Config) (*sql.DB, error)
	// ParseError wraps a driver error so its message stays generic (see WrapError).
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

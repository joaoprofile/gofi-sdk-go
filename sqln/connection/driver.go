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

// Settings holds the structured connection parameters that a Driver assembles
// into its driver-specific DSN. It decouples DSN construction from any single
// configuration source: gofi's config.Database fills it from the DATABASE_*
// environment variables, but callers can build it by hand just as well.
type Settings struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type Driver interface {
	Name() DriverName
	// DSN assembles the driver-specific connection string from s. Each driver
	// owns its own format (key-value for postgres, URL for sqlserver, …) so new
	// databases can be added without a central switch.
	DSN(s Settings) string
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

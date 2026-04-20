package migrate

import (
	"database/sql"

	"github.com/golang-migrate/migrate/v4/database"
)

type Driver interface {
	Name() string
	Instance(db *sql.DB) (database.Driver, error)
}

var drivers = map[string]Driver{}

func RegisterDriver(d Driver) {
	drivers[d.Name()] = d
}

func getDriver(name string) (Driver, bool) {
	d, ok := drivers[name]
	return d, ok
}

func GetDriver(name string) (Driver, bool) {
	return getDriver(name)
}

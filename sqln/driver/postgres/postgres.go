package postgres

import (
	"database/sql"

	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/driver"
	_ "github.com/lib/pq"
)

type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverPostgres
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open(string(connection.DriverPostgres), cfg.DSN)
}

func (Driver) ParseError(err error) error {
	return err
}

func (Driver) Dialect() driver.Dialect {
	return PostgresDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}

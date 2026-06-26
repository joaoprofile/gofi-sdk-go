package postgres

import (
	"database/sql"
	"fmt"

	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/driver"
	_ "github.com/lib/pq"
)

type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverPostgres
}

// DSN builds a lib/pq key-value connection string. An empty SSLMode defaults to
// "disable" so the resulting DSN is always valid.
func (Driver) DSN(s connection.Settings) string {
	sslmode := s.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		s.Host, s.Port, s.User, s.Password, s.Name, sslmode,
	)
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

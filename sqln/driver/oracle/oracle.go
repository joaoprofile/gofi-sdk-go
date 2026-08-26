package oracle

import (
	"database/sql"
	"fmt"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// Oracle driver. To enable it, blank-import this package:
//
//	import _ "github.com/joaoprofile/gofi/sqln/driver/oracle"
//
// Requires godror or go-oci8 in go.mod.
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverOracle
}

// DSN builds a go-ora URL: oracle://user:pass@host:port/service.
func (Driver) DSN(s connection.Settings) string {
	return fmt.Sprintf("oracle://%s:%s@%s:%d/%s", s.User, s.Password, s.Host, s.Port, s.Name)
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open("oracle", cfg.DSN)
}

func (Driver) ParseError(err error) error {
	return err
}

func (Driver) Dialect() sqln_driver.Dialect {
	return OracleDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}

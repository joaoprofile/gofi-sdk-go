package oracle

import (
	"database/sql"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// Driver Oracle. Para ativar, importe com blank import:
//
//	import _ "github.com/joaoprofile/gofi/sqln/driver/oracle"
//
// Requer godror ou go-oci8 no go.mod.
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverOracle
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

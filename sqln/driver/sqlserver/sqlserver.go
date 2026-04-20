package sqlserver

import (
	"database/sql"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// Driver SQL Server. Para ativar, importe com blank import:
//
//	import _ "github.com/joaoprofile/gofi/sqln/driver/sqlserver"
//
// Requer github.com/denisenkom/go-mssqldb no go.mod:
//
//	go get github.com/denisenkom/go-mssqldb
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverSQLServer
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open("sqlserver", cfg.DSN)
}

func (Driver) ParseError(err error) error {
	return err
}

func (Driver) Dialect() sqln_driver.Dialect {
	return SQLServerDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}

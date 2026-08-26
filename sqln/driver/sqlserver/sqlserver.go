package sqlserver

import (
	"database/sql"
	"fmt"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// SQL Server driver. To enable it, blank-import this package:
//
//	import _ "github.com/joaoprofile/gofi/sqln/driver/sqlserver"
//
// Requires github.com/denisenkom/go-mssqldb in go.mod:
//
//	go get github.com/denisenkom/go-mssqldb
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverSQLServer
}

// DSN builds a go-mssqldb URL: sqlserver://user:pass@host:port?database=dbname.
func (Driver) DSN(s connection.Settings) string {
	return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s", s.User, s.Password, s.Host, s.Port, s.Name)
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

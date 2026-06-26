package mysql

import (
	"database/sql"
	"fmt"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
)

// Driver MySQL. Para ativar, importe este pacote com blank import:
//
//	import _ "github.com/joaoprofile/gofi/sqln/driver/mysql"
//
// Requer o driver go-sql-driver/mysql no go.mod:
//
//	go get github.com/go-sql-driver/mysql
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverMySQL
}

// DSN builds a go-sql-driver/mysql DSN: user:pass@tcp(host:port)/dbname.
func (Driver) DSN(s connection.Settings) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", s.User, s.Password, s.Host, s.Port, s.Name)
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open("mysql", cfg.DSN)
}

func (Driver) ParseError(err error) error {
	return err
}

func (Driver) Dialect() sqln_driver.Dialect {
	return MySQLDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}

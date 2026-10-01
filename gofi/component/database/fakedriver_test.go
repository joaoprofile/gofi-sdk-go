package database

import (
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"sync"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	sqlndriver "github.com/gofi-labs/gofi-sdk-go/sqln/driver"
)

// fakeDriverName is a sqln driver backed by a database/sql driver whose
// connections always ping, so the open path runs without a database server.
const fakeDriverName = "gofi-component-fake"

var registerFake sync.Once

func registerFakeDriver() {
	registerFake.Do(func() {
		sql.Register(fakeDriverName, fakeSQLDriver{})
		connection.RegisterDriver(fakeConnDriver{})
	})
}

type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(string) (sqldriver.Conn, error) { return fakeConn{}, nil }

type fakeConn struct{}

var errUnsupported = errors.New("fake driver: not supported")

func (fakeConn) Prepare(string) (sqldriver.Stmt, error) { return nil, errUnsupported }
func (fakeConn) Close() error                           { return nil }
func (fakeConn) Begin() (sqldriver.Tx, error)           { return nil, errUnsupported }

type fakeConnDriver struct{}

func (fakeConnDriver) Name() connection.DriverName      { return fakeDriverName }
func (fakeConnDriver) DSN(s connection.Settings) string { return "host=" + s.Host }
func (fakeConnDriver) Open(cfg connection.Config) (*sql.DB, error) {
	return sql.Open(fakeDriverName, cfg.DSN)
}
func (fakeConnDriver) ParseError(err error) error  { return err }
func (fakeConnDriver) Dialect() sqlndriver.Dialect { return nil }

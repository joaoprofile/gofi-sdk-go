package connection

import (
	"database/sql"
	"fmt"
	"sync/atomic"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

var globalConn atomic.Pointer[Connection]

// SetGlobal sets the process-wide connection; the first call wins.
func SetGlobal(conn *Connection) {
	globalConn.CompareAndSwap(nil, conn)
}

func Global() (*Connection, error) {
	conn := globalConn.Load()
	if conn == nil {
		return nil, fmt.Errorf("sqln global connection not initialized")
	}

	return conn, nil
}

func DB() (*sql.DB, error) {
	conn, err := Global()
	if err != nil {
		return nil, err
	}

	return conn.DB(), nil
}

func MustDB() *sql.DB {
	conn, err := Global()
	if err != nil {
		panic(err)
	}

	return conn.DB()
}

// Dialect returns the SQL dialect of the active global connection.
// Returns nil if no connection has been established yet.
func Dialect() driver.Dialect {
	conn := globalConn.Load()
	if conn == nil {
		return nil
	}
	return conn.Dialect()
}

// ResetGlobalForTest resets the global connection state.
// Must only be called from tests; not safe for concurrent use.
func ResetGlobalForTest() {
	globalConn.Store(nil)
}

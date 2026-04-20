package connection

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/joaoprofile/gofi/sqln/driver"
)

var (
	globalConn *Connection
	onceGlobal sync.Once
)

func SetGlobal(conn *Connection) {
	onceGlobal.Do(func() {
		globalConn = conn
	})
}

func Global() (*Connection, error) {
	if globalConn == nil {
		return nil, fmt.Errorf("sqln global connection not initialized")
	}

	return globalConn, nil
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
	if globalConn == nil {
		return nil
	}
	return globalConn.Dialect()
}

// ResetGlobalForTest resets the global connection state.
// Must only be called from tests; not safe for concurrent use.
func ResetGlobalForTest() {
	globalConn = nil
	onceGlobal = sync.Once{}
}

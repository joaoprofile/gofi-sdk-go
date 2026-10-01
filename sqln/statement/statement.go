package statement

import (
	"context"
	"database/sql"
	"errors"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
)

// Statement runs write statements and single-row queries on the global
// connection, joining the transaction carried by ctx when there is one.
type Statement interface {
	// Execute runs query directly (no server-side prepare, PgBouncer-safe) and
	// returns the result, e.g. for RowsAffected in optimistic locking.
	Execute(ctx context.Context, query string, args ...any) (sql.Result, error)
	Prepare(ctx context.Context, query string) (*sql.Stmt, error)
	// QueryRow returns an error instead of panicking when the connection is
	// missing or the query is empty.
	QueryRow(ctx context.Context, query string, args ...any) (*sql.Row, error)
}

type statement struct {
	conn *connection.Connection // nil uses the global connection
}

// NewStatement runs on the global connection.
func NewStatement() Statement {
	return &statement{}
}

// NewWithConnection runs on conn, for services with more than one database.
func NewWithConnection(conn *connection.Connection) Statement {
	return &statement{conn: conn}
}

// QueryRow is not bounded by the connection's QueryTimeout (the row is read
// after it returns) and its Scan returns the driver error as is.
func (s *statement) QueryRow(ctx context.Context, query string, args ...any) (*sql.Row, error) {
	conn, err := s.validate(query)
	if err != nil {
		return nil, err
	}
	return connection.QuerierFrom(ctx, conn.DB()).QueryRowContext(ctx, query, args...), nil
}

// Execute is bounded by the connection's QueryTimeout when ctx has no
// deadline; driver errors are wrapped in *connection.Error.
func (s *statement) Execute(ctx context.Context, query string, args ...any) (sql.Result, error) {
	conn, err := s.validate(query)
	if err != nil {
		return nil, err
	}
	ctx, cancel := conn.WithQueryTimeout(ctx)
	defer cancel()
	res, err := connection.QuerierFrom(ctx, conn.DB()).ExecContext(ctx, query, args...)
	return res, connection.WrapError("exec", err)
}

// Prepare bounds only the prepare round trip by QueryTimeout.
func (s *statement) Prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	conn, err := s.validate(query)
	if err != nil {
		return nil, err
	}
	ctx, cancel := conn.WithQueryTimeout(ctx)
	defer cancel()
	stmt, err := connection.QuerierFrom(ctx, conn.DB()).PrepareContext(ctx, query)
	return stmt, connection.WrapError("prepare", err)
}

func (s *statement) validate(query string) (*connection.Connection, error) {
	conn := s.conn
	if conn == nil {
		var err error
		if conn, err = connection.Global(); err != nil {
			return nil, errors.New(connection.ErrDatabaseNotInitialized)
		}
	}
	if query == "" {
		return nil, errors.New(connection.ErrQueryIsEmpty)
	}
	return conn, nil
}

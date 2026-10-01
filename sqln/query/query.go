package query

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
)

type SQLQuery struct {
	Query  string
	Params []any
}

type Query interface {
	FetchRows(ctx context.Context, dbConn *sql.DB, query string, args ...any) (*sql.Rows, error)
	FetchRow(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row
	Execute(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row
}

type queryImpl struct{}

func NewQuery() Query {
	return &queryImpl{}
}

// FetchRows wraps driver errors in *connection.Error (generic message; the
// driver error stays reachable with errors.As).
func (q *queryImpl) FetchRows(ctx context.Context, dbConn *sql.DB, query string, args ...any) (*sql.Rows, error) {
	if err := q.validate(dbConn, query); err != nil {
		return nil, err
	}

	rows, err := connection.QuerierFrom(ctx, dbConn).QueryContext(ctx, query, args...)
	return rows, connection.WrapError("query", err)
}

func (q *queryImpl) FetchRow(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row {
	return connection.QuerierFrom(ctx, dbConn).QueryRowContext(ctx, query, args...)
}

func (q *queryImpl) Execute(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row {
	return connection.QuerierFrom(ctx, dbConn).QueryRowContext(ctx, query, args...)
}

func (q *queryImpl) validate(db *sql.DB, query string) error {
	if db == nil {
		return errors.New(connection.ErrDatabaseNotInitialized)
	}
	if query == "" {
		return errors.New(connection.ErrQueryIsEmpty)
	}
	return nil
}

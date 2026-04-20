package query

import (
	"context"
	"database/sql"
	"errors"

	"github.com/joaoprofile/gofi/sqln/connection"
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

func (q *queryImpl) FetchRows(ctx context.Context, dbConn *sql.DB, query string, args ...any) (*sql.Rows, error) {
	if err := q.validate(dbConn, query); err != nil {
		return nil, err
	}

	if tx := ctx.Value(connection.SqlTxContextKey); tx != nil {
		return tx.(*sql.Tx).QueryContext(ctx, query, args...)
	}
	return dbConn.QueryContext(ctx, query, args...)
}

func (q *queryImpl) FetchRow(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row {
	if tx := ctx.Value(connection.SqlTxContextKey); tx != nil {
		return tx.(*sql.Tx).QueryRowContext(ctx, query, args...)
	}
	return dbConn.QueryRowContext(ctx, query, args...)
}

func (q *queryImpl) Execute(ctx context.Context, dbConn *sql.DB, query string, args ...any) *sql.Row {
	if tx := ctx.Value(connection.SqlTxContextKey); tx != nil {
		return tx.(*sql.Tx).QueryRowContext(ctx, query, args...)
	}
	return dbConn.QueryRowContext(ctx, query, args...)
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

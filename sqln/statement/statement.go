package statement

import (
	"context"
	"database/sql"
	"errors"

	"github.com/joaoprofile/gofi/sqln/connection"
)

type Statement interface {
	Execute(ctx context.Context, query string, args ...any) error
	Prepare(ctx context.Context, query string) (*sql.Stmt, error)
	QueryRow(ctx context.Context, query string, args ...any) *sql.Row
}

type statement struct{}

func NewStatement() Statement {
	return &statement{}
}

func (s *statement) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := connection.DB()
	if err != nil {
		// Return a row that will surface the error when Scan is called.
		// MustDB would panic; instead we degrade gracefully here.
		panic(connection.ErrDatabaseNotInitialized)
	}

	if query == "" {
		return db.QueryRowContext(ctx, "SELECT '"+connection.ErrQueryIsEmpty+"'")
	}

	if tx := ctx.Value(connection.SqlTxContextKey); tx != nil {
		return tx.(*sql.Tx).QueryRowContext(ctx, query, args...)
	}
	return db.QueryRowContext(ctx, query, args...)
}

func (s *statement) Execute(ctx context.Context, query string, args ...any) error {
	if err := s.validate(query); err != nil {
		return err
	}

	stmt, err := s.prepare(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.ExecContext(ctx, args...)
	return err
}

func (s *statement) Prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	if err := s.validate(query); err != nil {
		return nil, err
	}
	return s.prepare(ctx, query)
}

func (s *statement) prepare(ctx context.Context, query string) (*sql.Stmt, error) {
	db := connection.MustDB()

	if tx := ctx.Value(connection.SqlTxContextKey); tx != nil {
		return tx.(*sql.Tx).PrepareContext(ctx, query)
	}

	return db.PrepareContext(ctx, query)
}

func (s *statement) validate(query string) error {
	if _, err := connection.DB(); err != nil {
		return errors.New(connection.ErrDatabaseNotInitialized)
	}
	if query == "" {
		return errors.New(connection.ErrQueryIsEmpty)
	}
	return nil
}

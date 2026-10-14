package fts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Policy decides what a connection does before running a search on an index.
type Policy uint8

const (
	// Verify (default) checks the catalog once per index and connection and
	// fails the search with ErrIndexMissing, ErrIndexOutdated or
	// ErrServerConfig when the index is not there as defined.
	Verify Policy = iota
	// Ensure creates a missing index, then verifies it. Meant for development
	// and tests; production indexes belong in migrations (see Migration).
	Ensure
	// Trust runs searches without checking, e.g. against a mocked database.
	Trust
)

// Querier is the part of *sql.DB, *sql.Tx and *sql.Conn the checks use.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Check verifies that each index exists in db as defined in d, and that the
// server settings it depends on match.
func Check(ctx context.Context, db Querier, d driver.Dialect, indexes ...*Index) error {
	fd, err := fullText(d)
	if err != nil {
		return err
	}
	var errs []error
	for _, idx := range indexes {
		errs = append(errs, check(ctx, db, d, fd, idx))
	}
	return errors.Join(errs...)
}

// Create creates each missing index with the statements of Migration, then
// checks all of them; an outdated index is reported, never dropped.
func Create(ctx context.Context, db Querier, d driver.Dialect, indexes ...*Index) error {
	fd, err := fullText(d)
	if err != nil {
		return err
	}
	var errs []error
	for _, idx := range indexes {
		err := check(ctx, db, d, fd, idx)
		if errors.Is(err, ErrIndexMissing) {
			err = create(ctx, db, d, idx)
			if err == nil {
				err = check(ctx, db, d, fd, idx)
			}
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Apply runs policy p on the indexes: Check, Create or nothing.
func Apply(ctx context.Context, p Policy, db Querier, d driver.Dialect, indexes ...*Index) error {
	switch p {
	case Trust:
		return nil
	case Ensure:
		return Create(ctx, db, d, indexes...)
	default:
		return Check(ctx, db, d, indexes...)
	}
}

func check(ctx context.Context, db Querier, d driver.Dialect, fd driver.FullTextDialect, idx *Index) error {
	if idx == nil {
		return fmt.Errorf("%w: nil index", ErrInvalidIndex)
	}
	want, err := idx.Comment(d)
	if err != nil {
		return err
	}
	query, args := fd.TextIndexLookup(&idx.def)
	var got string
	switch err := db.QueryRowContext(ctx, query, args...).Scan(&got); {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %s on %s", ErrIndexMissing, idx.def.Name, idx.def.Table)
	case err != nil:
		return fmt.Errorf("fts: looking up %s: %w", idx.def.Name, err)
	case got != want:
		return fmt.Errorf("%w: %s is tagged %q, the definition renders %q", ErrIndexOutdated, idx.def.Name, got, want)
	}
	for _, c := range fd.TextChecks(&idx.def) {
		var v string
		if err := db.QueryRowContext(ctx, c.Query, c.Args...).Scan(&v); err != nil {
			return fmt.Errorf("fts: checking %s for %s: %w", c.What, idx.def.Name, err)
		}
		if v != c.Want {
			return fmt.Errorf("%w: %s is %s, %s needs %s", ErrServerConfig, c.What, v, idx.def.Name, c.Want)
		}
	}
	return nil
}

func create(ctx context.Context, db Querier, d driver.Dialect, idx *Index) error {
	up, _, err := Statements(d, idx)
	if err != nil {
		return err
	}
	for _, stmt := range up {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("fts: creating %s: %w", idx.def.Name, err)
		}
	}
	return nil
}

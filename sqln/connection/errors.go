package connection

import (
	"context"
	"errors"
)

// Shared error message constants used across sqln subpackages.
const (
	ErrDatabaseNotInitialized = "database connection not initialized"
	ErrQueryIsEmpty           = "query is empty"
)

// Error hides a database error behind a generic message: driver messages
// carry table, column and constraint names and, in details, row values that
// must not reach API clients. errors.As/Is still reach the driver error
// (e.g. *pgconn.PgError, sql.ErrNoRows) and SQLState keeps the code.
type Error struct {
	Op  string // query, exec, prepare, scan
	Err error
}

func (e *Error) Error() string {
	msg := "sqln: " + e.Op + " failed"
	for _, ctxErr := range []error{context.DeadlineExceeded, context.Canceled} {
		if errors.Is(e.Err, ctxErr) {
			return msg + ": " + ctxErr.Error()
		}
	}
	if code := e.SQLState(); code != "" {
		msg += " (SQLSTATE " + code + ")"
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// SQLState returns the SQLSTATE code of the driver error, or "".
func (e *Error) SQLState() string {
	var st interface{ SQLState() string }
	if errors.As(e.Err, &st) {
		return st.SQLState()
	}
	return ""
}

// WrapError wraps a driver error returned by op in an *Error; nil and errors
// already wrapped are returned as is.
func WrapError(op string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}
	return &Error{Op: op, Err: err}
}

// Cause returns the driver error behind err's *Error, for logs; other
// errors are returned as is.
func Cause(err error) error {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Err
	}
	return err
}

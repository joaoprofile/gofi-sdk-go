package connection

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATE codes of the integrity violations repositories translate into
// domain errors.
const (
	SQLStateNotNullViolation    = "23502"
	SQLStateForeignKeyViolation = "23503"
	SQLStateUniqueViolation     = "23505"
	SQLStateCheckViolation      = "23514"
)

// SQLState returns the SQLSTATE code of the database error in err's chain,
// through *Error and any other wrapping, or "" when there is none. Callers do
// not need to know which driver sqln uses.
func SQLState(err error) string {
	code, _ := pgErrorFields(err)
	return code
}

// ConstraintName returns the constraint named by the Postgres error in err's
// chain, or "".
func ConstraintName(err error) string {
	_, constraint := pgErrorFields(err)
	return constraint
}

// IsUniqueViolation reports a unique violation (23505), optionally only on the
// given constraint.
func IsUniqueViolation(err error, constraint ...string) bool {
	return isViolation(err, SQLStateUniqueViolation, constraint)
}

// IsCheckViolation reports a check violation (23514), optionally only on the
// given constraint.
func IsCheckViolation(err error, constraint ...string) bool {
	return isViolation(err, SQLStateCheckViolation, constraint)
}

// IsForeignKeyViolation reports a foreign key violation (23503), optionally
// only on the given constraint.
func IsForeignKeyViolation(err error, constraint ...string) bool {
	return isViolation(err, SQLStateForeignKeyViolation, constraint)
}

// IsNotNullViolation reports a not-null violation (23502).
func IsNotNullViolation(err error) bool {
	return isViolation(err, SQLStateNotNullViolation, nil)
}

func isViolation(err error, code string, constraint []string) bool {
	got, name := pgErrorFields(err)
	if got != code {
		return false
	}
	return len(constraint) == 0 || constraint[0] == "" || name == constraint[0]
}

// pgErrorFields reads the code and constraint of the driver error. Errors that
// only expose SQLState() (another driver) still give their code.
func pgErrorFields(err error) (code, constraint string) {
	if err == nil {
		return "", ""
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code, pgErr.ConstraintName
	}
	var st interface{ SQLState() string }
	if errors.As(err, &st) {
		return st.SQLState(), ""
	}
	return "", ""
}

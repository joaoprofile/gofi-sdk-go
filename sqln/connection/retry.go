package connection

import "errors"

// ErrPasswordFuncUnsupported is returned by drivers that cannot use Config.Password.
var ErrPasswordFuncUnsupported = errors.New("sqln: Config.Password is not supported by this driver")

// Retryable SQLSTATE codes: serialization failure (standard) and deadlock
// detected (PostgreSQL).
var retryableStates = map[string]bool{"40001": true, "40P01": true}

// IsRetryable reports whether err is a transient transaction conflict that
// succeeds when the whole transaction runs again. It recognises errors that
// expose SQLState() (pgx, lib/pq).
func IsRetryable(err error) bool {
	var st interface{ SQLState() string }
	return errors.As(err, &st) && retryableStates[st.SQLState()]
}

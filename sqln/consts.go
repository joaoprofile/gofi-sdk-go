package sqln

import (
	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/pagination"
)

// SortDirection re-exported from pagination for backward compatibility.
type SortDirection = pagination.SortDirection

const (
	ASC  SortDirection = pagination.ASC
	DESC SortDirection = pagination.DESC
)

const (
	DefaultPage          = pagination.DefaultPage
	DefaultLimit         = pagination.DefaultLimit
	DefaultSortField     = pagination.DefaultSortField
	DefaultSortDirection = pagination.DefaultSortDirection
)

// SqlTxContextKey re-exported from connection for backward compatibility.
// transaction.Execute stores the active *sql.Tx under this key.
const SqlTxContextKey = connection.SqlTxContextKey

const (
	MsgTransactionIsolationIgnored = "transaction isolation only uses the first parameter, others are ignored"
	ErrExecuteTransaction          = "error when executing transaction error: %w"
	ErrTransactionRollback         = "error when executing transaction rollback: %v, original error: %w"
	ErrTransactionCommit           = "could not commit transaction: %w"
	ErrTransactionStart            = "could not start database transaction: %v"
)

// Re-exported from connection for backward compatibility.
const (
	ErrDatabaseNotInitialized = connection.ErrDatabaseNotInitialized
	ErrMsgQueryIsEmpty        = connection.ErrQueryIsEmpty
)

const (
	ErrMsgFailedConnect        string = "failed to connect to PostgreSQL: %w"
	ErrMsgFailedConnectionPing string = "failed to connect to PostgreSQL ping: %w"
	ErrMsgMigration            string = "an error occurred when validate database migrations: %v"
	ErrPageIsEmpty             string = "page is empty, please create a pageable query"
	ErrMsgStmClose             string = "failed to close statement: %v\n"
)

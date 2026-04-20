package connection

// txContextKey is unexported to prevent key collisions with other packages.
type txContextKey string

// SqlTxContextKey is the context key used to propagate *sql.Tx within a request.
// transaction.Execute stores the active transaction under this key; Statement
// and Query read it to participate in the same transaction automatically.
const SqlTxContextKey txContextKey = "sqlTxContext"

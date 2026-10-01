package sqln

import (
	"database/sql"

	"github.com/gofi-labs/gofi-sdk-go/sqln/transaction"
)

type Transaction = transaction.Transaction

func NewTransaction(isolation ...sql.IsolationLevel) Transaction {
	return transaction.NewTransaction(isolation...)
}

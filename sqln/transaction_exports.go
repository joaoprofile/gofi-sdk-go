package sqln

import (
	"database/sql"

	"github.com/joaoprofile/gofi/sqln/transaction"
)

type Transaction = transaction.Transaction

func NewTransaction(isolation ...sql.IsolationLevel) Transaction {
	return transaction.NewTransaction(isolation...)
}

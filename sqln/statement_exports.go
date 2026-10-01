package sqln

import "github.com/gofi-labs/gofi-sdk-go/sqln/statement"

type Statement = statement.Statement

func NewStatement() Statement { return statement.NewStatement() }

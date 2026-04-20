package sqln

import "github.com/joaoprofile/gofi/sqln/statement"

type Statement = statement.Statement

func NewStatement() Statement { return statement.NewStatement() }

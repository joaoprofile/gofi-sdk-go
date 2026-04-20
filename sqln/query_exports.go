package sqln

import "github.com/joaoprofile/gofi/sqln/query"

type Query = query.Query

type SQLQuery = query.SQLQuery

func NewQuery() Query { return query.NewQuery() }

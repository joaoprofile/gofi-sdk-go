package sqln

import "github.com/joaoprofile/gofi-sdk-go/sqln/query"

type Query = query.Query

type SQLQuery = query.SQLQuery

func NewQuery() Query { return query.NewQuery() }

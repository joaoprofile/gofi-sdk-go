package criteria

import "github.com/joaoprofile/gofi/sqln/driver"

// BuildClause compiles a predicate slice into a SQL WHERE fragment and its bound parameters.
func BuildClause(predicates []Predicate, d driver.FilterDialect) (string, []any) {
	b := newBuilder(d)
	return b.buildClause(predicates), b.params
}

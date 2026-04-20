package sqln

import (
	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/joaoprofile/gofi/sqln/driver"
)

// BuildClause compiles a predicate slice into a SQL WHERE fragment and its bound parameters.
// The fragment does NOT include the WHERE keyword — designed for embedding into an existing
// base query (e.g., base + " AND ( " + clause + " )").
// Adjacent predicates without an explicit And()/Or() connector are implicitly joined with AND.
func BuildClause(predicates []Predicate, dialect driver.FilterDialect) (string, []any) {
	return criteria.BuildClause(predicates, dialect)
}

// Types

// CriteriaQuery is the declarative SQL query builder.
// Use CriteriaFrom() as the entry point, or import the criteria sub-package directly
type CriteriaQuery = criteria.Query

// Predicate represents a WHERE/HAVING condition or a logical connector (AND/OR).
type Predicate = criteria.Predicate

// CriteriaOrder defines a sort expression for a criteria query.
type CriteriaOrder = criteria.Order

// Entry Point
func CriteriaFrom(table, alias string) *CriteriaQuery {
	return criteria.From(table, alias)
}

// Order Helpers

// Asc creates an ascending ORDER BY expression for criteria queries.
func Asc(field string) CriteriaOrder { return criteria.Asc(field) }

// Desc creates a descending ORDER BY expression for criteria queries.
func Desc(field string) CriteriaOrder { return criteria.Desc(field) }

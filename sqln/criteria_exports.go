package sqln

import (
	"github.com/gofi-labs/gofi-sdk-go/sqln/criteria"
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver"
)

// BuildClause compiles a predicate slice into a SQL WHERE fragment and its bound parameters.
// The fragment does NOT include the WHERE keyword — designed for embedding into an existing
// base query (e.g., base + " AND ( " + clause + " )").
// Adjacent predicates without an explicit And()/Or() connector are implicitly joined with AND.
// It fails with criteria.ErrInvalidField when a field is not a column reference.
func BuildClause(predicates []Predicate, dialect driver.FilterDialect) (string, []any, error) {
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

// Asc creates an ascending ORDER BY expression for criteria queries; a field
// that is not a column reference fails the build unless marked Raw.
func Asc(field string) CriteriaOrder { return criteria.Asc(field) }

// Desc creates a descending ORDER BY expression for criteria queries; see Asc.
func Desc(field string) CriteriaOrder { return criteria.Desc(field) }

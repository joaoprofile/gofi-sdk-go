package criteria

import (
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi/sqln/driver"
)

// builder accumulates bound parameters and generates SQL fragments.
// It is internal to the package and used exclusively by Query.Build.
type builder struct {
	dialect driver.FilterDialect
	params  []any
}

func newBuilder(d driver.FilterDialect) *builder {
	return &builder{dialect: d}
}

// nextParam appends value to the parameter list and returns the dialect-specific
// placeholder for it (e.g. $1, ?, :1, @p1).
func (b *builder) nextParam(value any) string {
	b.params = append(b.params, value)
	return b.dialect.Param(len(b.params))
}

// buildPredicate translates a single Predicate into a SQL fragment,
// binding any values as parameters.
func (b *builder) buildPredicate(p Predicate) string {
	switch p.operator {

	// Null / boolean checks — no bound parameter.
	case driver.IsNull, driver.IsNotNull, driver.IsTrue, driver.IsFalse:
		return fmt.Sprintf("%s %s", p.field, p.operator)

	// Group — wraps inner predicates in parentheses.
	case driver.Group:
		return "(" + b.buildClause(p.children) + ")"

	// Membership — expands slice into individual placeholders.
	case driver.In, driver.NotIn:
		return b.buildMembership(p)

	// Range — requires exactly two values.
	case driver.Between:
		return b.buildBetween(p)

	// Case-insensitive match — delegate to dialect (ILIKE on PG, LIKE elsewhere).
	case driver.Contains:
		return b.dialect.Like(p.field, b.nextParam(p.value))
	case driver.NotContains:
		return b.dialect.NotLike(p.field, b.nextParam(p.value))

	// Case-sensitive LIKE — literal SQL, not delegated to dialect.
	case driver.Like:
		return fmt.Sprintf("%s LIKE %s", p.field, b.nextParam(p.value))
	case driver.NotLike:
		return fmt.Sprintf("%s NOT LIKE %s", p.field, b.nextParam(p.value))

	// Comparison: =, !=, <, <=, >, >=
	default:
		return fmt.Sprintf("%s %s %s", p.field, p.operator, b.nextParam(p.value))
	}
}

func (b *builder) buildMembership(p Predicate) string {
	values, ok := toSlice(p.value)
	if !ok {
		// Single scalar treated as IN ($n) — still valid SQL.
		return fmt.Sprintf("%s %s (%s)", p.field, p.operator, b.nextParam(p.value))
	}

	placeholders := make([]string, len(values))
	for i, v := range values {
		placeholders[i] = b.nextParam(v)
	}

	return fmt.Sprintf("%s %s (%s)", p.field, p.operator, strings.Join(placeholders, ", "))
}

func (b *builder) buildBetween(p Predicate) string {
	values, ok := toSlice(p.value)
	if !ok || len(values) != 2 {
		panic("criteria: BETWEEN requires exactly two values — use criteria.Between(field, from, to)")
	}

	return fmt.Sprintf(
		"%s BETWEEN %s AND %s",
		p.field,
		b.nextParam(values[0]),
		b.nextParam(values[1]),
	)
}

// buildClause builds the body of a WHERE or HAVING clause from a predicate list.
//
// Rules:
//   - Logical connectors (And/Or) are emitted verbatim.
//   - Adjacent conditions without an explicit connector are implicitly joined with AND.
//   - A logical connector adjacent to another connector is preserved as-is
//     (validation of intent is the caller's responsibility).
func (b *builder) buildClause(predicates []Predicate) string {
	parts := make([]string, 0, len(predicates)*2)

	for i, p := range predicates {
		if p.isLogical() {
			parts = append(parts, p.logical)
			continue
		}

		// Insert implicit AND when the previous token was a condition (not a connector).
		if i > 0 && !predicates[i-1].isLogical() {
			parts = append(parts, driver.And)
		}

		parts = append(parts, b.buildPredicate(p))
	}

	return strings.Join(parts, " ")
}

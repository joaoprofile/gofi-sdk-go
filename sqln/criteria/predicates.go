package criteria

import (
	"fmt"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Predicate represents a single WHERE/HAVING condition or a logical connector (AND/OR).
// Use the constructor functions below — never build Predicate literals directly.
//
// The field must be a column reference (col, t.col, "Col"; see driver.IsIdentifier):
// it is written into the SQL text, so anything else fails Build with ErrInvalidField.
// Mark a trusted SQL expression such as COUNT(*) with Raw.
type Predicate struct {
	field    string
	operator string
	value    any
	logical  string      // non-empty only for logical connectors (AND / OR)
	children []Predicate // non-empty only for Group predicate
	raw      bool        // field is a trusted expression, written without validation
	literal  bool        // LIKE value is literal text matched as a substring
	err      error       // invalid constructor input, reported by Build
}

func (p Predicate) isLogical() bool { return p.logical != "" }

// Substring makes a Contains, NotContains, Like or NotLike value literal
// text matched anywhere in the field: its wildcards are escaped for the
// dialect, it is wrapped in %…% and ESCAPE '!' is appended. Use it for user
// input, so "%" or "_" cannot turn a search into a full scan pattern.
func (p Predicate) Substring() Predicate {
	p.literal = true
	return p
}

// Raw marks the field as a trusted SQL expression (e.g. COUNT(*), lower(u.name))
// written as is. Trusted input only — never user data.
func (p Predicate) Raw() Predicate {
	p.raw = true
	return p
}

// Comparison

// Eq produces field = value.
func Eq(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.Eq, value: value}
}

// Ne produces field != value.
func Ne(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.NotEqual, value: value}
}

// Lt produces field < value.
func Lt(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.Less, value: value}
}

// Lte produces field <= value.
func Lte(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.LessOrEqual, value: value}
}

// Gt produces field > value.
func Gt(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.Greater, value: value}
}

// Gte produces field >= value.
func Gte(field string, value any) Predicate {
	return Predicate{field: field, operator: driver.GreaterOrEqual, value: value}
}

// Membership

// In produces field IN (v1, v2, …).
// values must be a slice: []string, []int, []int64, []int32, []float64, or []any.
func In(field string, values any) Predicate {
	return Predicate{field: field, operator: driver.In, value: values}
}

// NotIn produces field NOT IN (v1, v2, …).
func NotIn(field string, values any) Predicate {
	return Predicate{field: field, operator: driver.NotIn, value: values}
}

// Text Search

// Contains performs a case-insensitive substring match via the active dialect:
//   - PostgreSQL → field ILIKE $n
//   - All others → field LIKE $n
func Contains(field string, value string) Predicate {
	return Predicate{field: field, operator: driver.Contains, value: value}
}

// NotContains is the negated form of Contains:
//   - PostgreSQL → field NOT ILIKE $n
//   - All others → field NOT LIKE $n
func NotContains(field string, value string) Predicate {
	return Predicate{field: field, operator: driver.NotContains, value: value}
}

// Like performs a case-sensitive LIKE match: field LIKE $n (all databases).
func Like(field string, value string) Predicate {
	return Predicate{field: field, operator: driver.Like, value: value}
}

// NotLike performs a case-sensitive NOT LIKE match: field NOT LIKE $n (all databases).
func NotLike(field string, value string) Predicate {
	return Predicate{field: field, operator: driver.NotLike, value: value}
}

//  Range

// Between produces field BETWEEN $from AND $to.
func Between(field string, from, to any) Predicate {
	return Predicate{field: field, operator: driver.Between, value: []any{from, to}}
}

//  Null Check

// IsNull produces field IS NULL (no bound parameter).
func IsNull(field string) Predicate {
	return Predicate{field: field, operator: driver.IsNull}
}

// IsNotNull produces field IS NOT NULL (no bound parameter).
func IsNotNull(field string) Predicate {
	return Predicate{field: field, operator: driver.IsNotNull}
}

// Boolean Check

// IsTrue produces field IS TRUE (no bound parameter).
func IsTrue(field string) Predicate {
	return Predicate{field: field, operator: driver.IsTrue}
}

// IsFalse produces field IS FALSE (no bound parameter).
func IsFalse(field string) Predicate {
	return Predicate{field: field, operator: driver.IsFalse}
}

// Group

// Group wraps one or more predicates in parentheses, producing (pred1 AND/OR pred2 ...).
// Use it to isolate OR branches from surrounding AND conditions, e.g.:
// Produces: WHERE p.company_id = $1 AND (p.title ILIKE $2 OR p.sku = $3)
//
// TODO: Create tests to validate Group within Group
func Group(predicates ...Predicate) Predicate {
	return Predicate{operator: driver.Group, children: predicates}
}

// Logical Connectors

// And inserts an explicit AND connector between adjacent predicates.
// Adjacent predicates without any connector are implicitly ANDed, so And() is
// only needed when mixing AND and OR within the same clause.
func And() Predicate { return Predicate{logical: driver.And} }

// Or inserts an OR connector between adjacent predicates.
func Or() Predicate { return Predicate{logical: driver.Or} }

// Date Predicates
//
// A zero time.Time, or a DateBetween whose from is after to, makes Build
// fail with ErrInvalidValue instead of producing a query.

func datePredicate(name, field, operator string, date time.Time) Predicate {
	p := Predicate{field: field, operator: operator, value: date}
	if date.IsZero() {
		p.err = fmt.Errorf("%w: %s on %q requires a non-zero time", ErrInvalidValue, name, field)
	}
	return p
}

// DateEq produces field = $date.
func DateEq(field string, date time.Time) Predicate {
	return datePredicate("DateEq", field, driver.Eq, date)
}

// DateBefore produces field < $date.
func DateBefore(field string, date time.Time) Predicate {
	return datePredicate("DateBefore", field, driver.Less, date)
}

// DateAfter produces field > $date.
func DateAfter(field string, date time.Time) Predicate {
	return datePredicate("DateAfter", field, driver.Greater, date)
}

// DateOnOrBefore produces field <= $date.
func DateOnOrBefore(field string, date time.Time) Predicate {
	return datePredicate("DateOnOrBefore", field, driver.LessOrEqual, date)
}

// DateOnOrAfter produces field >= $date.
func DateOnOrAfter(field string, date time.Time) Predicate {
	return datePredicate("DateOnOrAfter", field, driver.GreaterOrEqual, date)
}

// DateBetween produces field BETWEEN $from AND $to.
// Build fails when either value is zero or from is after to.
func DateBetween(field string, from, to time.Time) Predicate {
	p := Predicate{field: field, operator: driver.Between, value: []any{from, to}}
	switch {
	case from.IsZero() || to.IsZero():
		p.err = fmt.Errorf("%w: DateBetween on %q requires non-zero times", ErrInvalidValue, field)
	case from.After(to):
		p.err = fmt.Errorf("%w: DateBetween on %q requires from <= to", ErrInvalidValue, field)
	}
	return p
}

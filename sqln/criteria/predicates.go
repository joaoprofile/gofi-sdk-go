package criteria

import (
	"time"

	"github.com/joaoprofile/gofi/sqln/driver"
)

// Predicate represents a single WHERE/HAVING condition or a logical connector (AND/OR).
// Use the constructor functions below — never build Predicate literals directly.
type Predicate struct {
	field    string
	operator string
	value    any
	logical  string      // non-empty only for logical connectors (AND / OR)
	children []Predicate // non-empty only for Group predicate
}

func (p Predicate) isLogical() bool { return p.logical != "" }

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
// All date predicates validate that the supplied time.Time is not zero.
// DateBetween additionally validates that from is not after to.
// Violations panic immediately — these are programmer errors, not runtime conditions.

// DateEq produces field = $date.
func DateEq(field string, date time.Time) Predicate {
	if date.IsZero() {
		panic("criteria: DateEq requires a non-zero time.Time")
	}
	return Predicate{field: field, operator: driver.Eq, value: date}
}

// DateBefore produces field < $date.
func DateBefore(field string, date time.Time) Predicate {
	if date.IsZero() {
		panic("criteria: DateBefore requires a non-zero time.Time")
	}
	return Predicate{field: field, operator: driver.Less, value: date}
}

// DateAfter produces field > $date.
func DateAfter(field string, date time.Time) Predicate {
	if date.IsZero() {
		panic("criteria: DateAfter requires a non-zero time.Time")
	}
	return Predicate{field: field, operator: driver.Greater, value: date}
}

// DateOnOrBefore produces field <= $date.
func DateOnOrBefore(field string, date time.Time) Predicate {
	if date.IsZero() {
		panic("criteria: DateOnOrBefore requires a non-zero time.Time")
	}
	return Predicate{field: field, operator: driver.LessOrEqual, value: date}
}

// DateOnOrAfter produces field >= $date.
func DateOnOrAfter(field string, date time.Time) Predicate {
	if date.IsZero() {
		panic("criteria: DateOnOrAfter requires a non-zero time.Time")
	}
	return Predicate{field: field, operator: driver.GreaterOrEqual, value: date}
}

// DateBetween produces field BETWEEN $from AND $to.
// Panics if either value is zero or if from is after to.
func DateBetween(field string, from, to time.Time) Predicate {
	switch {
	case from.IsZero() || to.IsZero():
		panic("criteria: DateBetween requires non-zero time.Time values")
	case from.After(to):
		panic("criteria: DateBetween requires from <= to")
	}
	return Predicate{field: field, operator: driver.Between, value: []any{from, to}}
}

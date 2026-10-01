package criteria

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/sqln/driver"
)

// builder accumulates bound parameters and writes SQL straight into sb.
// It is internal to the package and used exclusively by Query.Build.
type builder struct {
	dialect driver.FilterDialect
	params  []any
	sb      *strings.Builder
	err     error // first invalid field or value; the SQL is discarded when set
}

// ErrInvalidField is returned by Build when a predicate or order field is not
// a column reference and was not marked Raw.
var ErrInvalidField = errors.New("criteria: invalid field")

// ErrInvalidValue is returned by Build when a predicate value has the wrong shape.
var ErrInvalidValue = errors.New("criteria: invalid value")

func (b *builder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// checkField rejects a field that is neither a column reference nor marked raw.
func (b *builder) checkField(field string, raw bool) bool {
	if raw || driver.IsIdentifier(field) {
		return true
	}
	b.fail(fmt.Errorf("%w: %q is not a column reference (use Raw for a trusted expression)", ErrInvalidField, field))
	return false
}

func newBuilder(d driver.FilterDialect, sb *strings.Builder) *builder {
	return &builder{dialect: d, sb: sb}
}

// nextParam appends value to the parameter list and returns the dialect-specific
// placeholder for it (e.g. $1, ?, :1, @p1).
func (b *builder) nextParam(value any) string {
	b.params = append(b.params, value)
	return b.dialect.Param(len(b.params))
}

func (b *builder) write(parts ...string) {
	for _, p := range parts {
		b.sb.WriteString(p)
	}
}

// writePredicate translates a single Predicate into SQL, binding any values
// as parameters.
func (b *builder) writePredicate(p Predicate) {
	if p.err != nil {
		b.fail(p.err)
		return
	}
	if p.operator != driver.Group && !b.checkField(p.field, p.raw) {
		return
	}
	switch p.operator {

	// Null / boolean checks — no bound parameter.
	case driver.IsNull, driver.IsNotNull, driver.IsTrue, driver.IsFalse:
		b.write(p.field, " ", p.operator)

	// Group — wraps inner predicates in parentheses.
	case driver.Group:
		b.write("(")
		b.writeClause(p.children)
		b.write(")")

	// Membership — a typed slice binds as one array where the dialect allows.
	case driver.In, driver.NotIn:
		b.writeMembership(p)

	// Range — requires exactly two values.
	case driver.Between:
		values, ok := toSlice(p.value)
		if !ok || len(values) != 2 {
			b.fail(fmt.Errorf("%w: BETWEEN on %q requires exactly two values", ErrInvalidValue, p.field))
			return
		}
		from := b.nextParam(values[0])
		b.write(p.field, " BETWEEN ", from, " AND ", b.nextParam(values[1]))

	// Case-insensitive match — delegate to dialect (ILIKE on PG, LIKE elsewhere).
	case driver.Contains:
		b.write(b.dialect.Like(p.field, b.likeParam(p)))
		b.writeEscape(p)
	case driver.NotContains:
		b.write(b.dialect.NotLike(p.field, b.likeParam(p)))
		b.writeEscape(p)

	// Case-sensitive LIKE — literal SQL, not delegated to dialect.
	case driver.Like:
		b.write(p.field, " LIKE ", b.likeParam(p))
		b.writeEscape(p)
	case driver.NotLike:
		b.write(p.field, " NOT LIKE ", b.likeParam(p))
		b.writeEscape(p)

	// Comparison: =, !=, <, <=, >, >=
	default:
		b.write(p.field, " ", p.operator, " ", b.nextParam(p.value))
	}
}

// likeParam binds p's LIKE pattern: as given, or escaped and wrapped in %…%
// when p is a Substring match.
func (b *builder) likeParam(p Predicate) string {
	if !p.literal {
		return b.nextParam(p.value)
	}
	s, ok := p.value.(string)
	if !ok {
		b.fail(fmt.Errorf("%w: substring match on %q requires a string", ErrInvalidValue, p.field))
		return ""
	}
	return b.nextParam("%" + driver.EscapeLike(b.dialect, s) + "%")
}

func (b *builder) writeEscape(p Predicate) {
	if p.literal {
		b.write(driver.LikeEscapeClause)
	}
}

func (b *builder) writeMembership(p Predicate) {
	values, ok := toSlice(p.value)
	if !ok {
		// Single scalar treated as IN ($n) — still valid SQL.
		b.write(p.field, " ", p.operator, " (", b.nextParam(p.value), ")")
		return
	}

	if len(values) == 0 {
		// "IN ()" is invalid SQL: an empty IN matches nothing, an empty NOT IN matches everything.
		if p.operator == driver.NotIn {
			b.write("1 = 1")
		} else {
			b.write("1 = 0")
		}
		return
	}

	if ad, ok := b.dialect.(driver.ArrayDialect); ok && isTypedSlice(p.value) {
		b.write(ad.ArrayMembership(p.field, b.nextParam(p.value), p.operator == driver.NotIn))
		return
	}

	b.write(p.field, " ", p.operator, " (")
	for i, v := range values {
		if i > 0 {
			b.write(", ")
		}
		b.write(b.nextParam(v))
	}
	b.write(")")
}

// isTypedSlice reports whether v is a slice the driver can bind as an array:
// not []any (mixed element types) and not []byte (a scalar).
func isTypedSlice(v any) bool {
	t := reflect.TypeOf(v)
	return t != nil && t.Kind() == reflect.Slice &&
		t.Elem().Kind() != reflect.Interface && t.Elem().Kind() != reflect.Uint8
}

// writeClause writes the body of a WHERE or HAVING clause from a predicate list.
//
// Rules:
//   - Logical connectors (And/Or) are emitted verbatim.
//   - Adjacent conditions without an explicit connector are implicitly joined with AND.
//   - A logical connector adjacent to another connector is preserved as-is
//     (validation of intent is the caller's responsibility).
func (b *builder) writeClause(predicates []Predicate) {
	for i, p := range predicates {
		if i > 0 {
			b.write(" ")
		}
		if p.isLogical() {
			b.write(p.logical)
			continue
		}
		// Insert implicit AND when the previous token was a condition (not a connector).
		if i > 0 && !predicates[i-1].isLogical() {
			b.write(driver.And, " ")
		}
		b.writePredicate(p)
	}
}

package filter

import (
	"fmt"
	"reflect"
	"slices"
	"unicode/utf8"
)

// Default bounds of a filter request, so a client cannot make Build emit an
// arbitrarily large query.
const (
	DefaultMaxFilters    = 50   // conditions, AND/OR not counted
	DefaultMaxInValues   = 1000 // elements of a list value
	DefaultMaxLikeLength = 256  // characters of a LIKE value
)

// Option changes the bounds Build enforces; a value <= 0 keeps the default.
type Option func(*limits)

type limits struct {
	maxFilters, maxInValues, maxLikeLength int
}

// WithMaxFilters bounds the number of conditions.
func WithMaxFilters(n int) Option { return func(l *limits) { l.maxFilters = n } }

// WithMaxInValues bounds the elements of a list value (IN, NOT IN, …).
func WithMaxInValues(n int) Option { return func(l *limits) { l.maxInValues = n } }

// WithMaxLikeLength bounds the characters of a Contains/Like value.
func WithMaxLikeLength(n int) Option { return func(l *limits) { l.maxLikeLength = n } }

func newLimits(opts []Option) limits {
	l := limits{}
	for _, o := range opts {
		o(&l)
	}
	if l.maxFilters <= 0 {
		l.maxFilters = DefaultMaxFilters
	}
	if l.maxInValues <= 0 {
		l.maxInValues = DefaultMaxInValues
	}
	if l.maxLikeLength <= 0 {
		l.maxLikeLength = DefaultMaxLikeLength
	}
	return l
}

var likeConditions = []string{Contains, NotContains, Like, NotLike}

// countConditions counts the filters that are not AND/OR separators.
func countConditions(filters []*Filter) int {
	n := 0
	for _, f := range filters {
		if f != nil && f.LogicalOperator == "" {
			n++
		}
	}
	return n
}

// check rejects a value above the list or LIKE length bounds.
func (l limits) check(f *Filter) error {
	values := []any{f.Value}
	if rv := reflect.ValueOf(f.Value); rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		if rv.Len() > l.maxInValues {
			return fmt.Errorf("field %q: %d values exceed the limit of %d", f.Field, rv.Len(), l.maxInValues)
		}
		values, _ = toAnySlice(f.Value)
	}
	if !slices.Contains(likeConditions, f.Condition) {
		return nil
	}
	for _, v := range values {
		if s, ok := v.(string); ok && utf8.RuneCountInString(s) > l.maxLikeLength {
			return fmt.Errorf("field %q: value exceeds %d characters", f.Field, l.maxLikeLength)
		}
	}
	return nil
}

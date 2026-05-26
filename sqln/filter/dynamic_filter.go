package filter

import (
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/joaoprofile/gofi/sqln/driver"
)

const timeLayout = time.RFC3339

const (
	defaultPage          = uint16(0)
	defaultLimit         = uint16(15)
	defaultSortDirection = "ASC"
)

type FilterDialect = driver.FilterDialect

// activeDialect returns the dialect of the active global database connection.
// Panics if no connection has been established — call connection.SetGlobal before using
// NewQueryBuild, or use NewQueryBuildWithDialect with an explicit dialect.
func activeDialect() FilterDialect {
	d := connection.Dialect()
	if d == nil {
		panic("sqln/filter: no active database connection — call connection.SetGlobal before using NewQueryBuild, or use NewQueryBuildWithDialect with an explicit dialect")
	}
	return d
}

const (
	Eq             = driver.Eq
	NotEqual       = driver.NotEqual
	Less           = driver.Less
	LessOrEqual    = driver.LessOrEqual
	Greater        = driver.Greater
	GreaterOrEqual = driver.GreaterOrEqual
)

const (
	In    = driver.In
	NotIn = driver.NotIn
)

const (
	Contains    = driver.Contains
	NotContains = driver.NotContains
	Like        = driver.Like
	NotLike     = driver.NotLike
)

const Between = driver.Between

const (
	IsNull    = driver.IsNull
	IsNotNull = driver.IsNotNull
)

const (
	And = driver.And
	Or  = driver.Or
)

// Value Type Wrappers
type StringValue string
type FloatValue float64
type IntValue int64
type SliceValue []any

// SQL Injection Guard
var notAllowedTerms = []string{
	"DROP", "DELETE", "UPDATE", "INSERT", "ALTER", "TRUNCATE",
	"EXEC", "EXECUTE", "MERGE", "UNION", "GRANT", "REVOKE",
	"DECLARE", "SLEEP", "BENCHMARK",
}

// notAllowedPatterns are pre-compiled word-boundary regexes for each blocked term.
var notAllowedPatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, len(notAllowedTerms))
	for i, term := range notAllowedTerms {
		patterns[i] = regexp.MustCompile(`(?i)\b` + term + `\b`)
	}
	return patterns
}()

// Error Messages

const (
	errMsgInvalidLogicalOp     = "invalid logical operator: %s"
	errMsgInvalidField         = "invalid field: %s"
	errMsgInvalidCondition     = "invalid condition: %s"
	errMsgUnsupportedTypeField = "unsupported value type for field"
	errMsgActionNotAllowed     = "action not allowed, invalid value for a filter: %s"
	errMsgInvalidSortingField  = "invalid sorting field: %s"
	errMsgBetweenRequiresPair  = "BETWEEN requires exactly 2 values for field: %s"
	errMsgConsecutiveLogicalOp = "consecutive logical operators are not allowed"
	errMsgLeadingLogicalOp     = "filter list cannot start with a logical operator"
	errMsgTrailingLogicalOp    = "filter list cannot end with a logical operator"
)

// Domain Types
type Filter struct {
	Field           string `json:"field,omitempty"`
	Condition       string `json:"condition,omitempty"`
	Value           any    `json:"value,omitempty"`
	LogicalOperator string `json:"logicalOperator,omitempty"`
}

func NewFilter(field, condition string, value any) *Filter {
	return &Filter{Field: field, Condition: condition, Value: value}
}

func AND() *Filter { return &Filter{LogicalOperator: And} }
func OR() *Filter  { return &Filter{LogicalOperator: Or} }

// FilterParams holds pagination and sorting parameters.
type FilterParams struct {
	Page          uint16 `json:"page"`
	Limit         uint16 `json:"limit"`
	SortField     string `json:"sortField"`
	SortDirection string `json:"sortDirection"`
}

// Filters is the root container for a dynamic query filter request.
type Filters struct {
	Tenant  any
	Params  *FilterParams `json:"params"`
	Filters []*Filter     `json:"filters"`
}

func NewFilters() *Filters {
	return &Filters{
		Params: &FilterParams{
			Page:          defaultPage,
			Limit:         defaultLimit,
			SortDirection: defaultSortDirection,
		},
		Filters: make([]*Filter, 0),
	}
}

func (f *Filters) Add(filter ...*Filter) *Filters {
	f.Filters = append(f.Filters, filter...)
	return f
}

// QueryParam holds the final SQL fragment and its bound parameters.
type QueryParam struct {
	Query  string
	Params []any
}

// Query Builder

func NewQueryBuild(query string, filter *Filters) *QueryParam {
	return filter.queryBuild(query, activeDialect())
}

func NewQueryBuildWithDialect(query string, filter *Filters, dialect FilterDialect) *QueryParam {
	if dialect == nil {
		dialect = activeDialect()
	}
	return filter.queryBuild(query, dialect)
}

func (filters *Filters) queryBuild(query string, d FilterDialect) *QueryParam {
	predicates := make([]criteria.Predicate, 0, len(filters.Filters))
	for _, f := range filters.Filters {
		if p, ok := filterToPredicate(f); ok {
			predicates = append(predicates, p)
		}
	}
	if len(predicates) == 0 {
		return &QueryParam{Query: query}
	}
	clause, params := criteria.BuildClause(predicates, d)
	return &QueryParam{
		Query:  fmt.Sprintf("%s AND ( %s )", query, clause),
		Params: params,
	}
}

// Predicate Conversion

// filterToPredicate converts a single *Filter into a criteria.Predicate.
// Returns (predicate, true) on success, or (zero, false) if the filter is invalid
// and should be skipped with a logged error.
func filterToPredicate(f *Filter) (criteria.Predicate, bool) {
	// Logical separator (AND / OR)
	if f.LogicalOperator != "" {
		switch f.LogicalOperator {
		case And:
			return criteria.And(), true
		case Or:
			return criteria.Or(), true
		default:
			logging.Error(errMsgInvalidLogicalOp, slog.String("op", f.LogicalOperator))
			return criteria.Predicate{}, false
		}
	}

	// Explicit null-check operators (no value required)
	if f.Condition == IsNull {
		return criteria.IsNull(f.Field), true
	}
	if f.Condition == IsNotNull {
		return criteria.IsNotNull(f.Field), true
	}

	// Implicit IS NULL when value is nil
	if f.Value == nil {
		return criteria.IsNull(f.Field), true
	}

	// Validate operator against the authoritative allowlist
	if _, ok := driver.AllowedOperators[f.Condition]; !ok {
		logging.Error(errMsgInvalidCondition, slog.String("condition", f.Condition))
		return criteria.Predicate{}, false
	}

	// Slice / array values
	rv := reflect.ValueOf(f.Value)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		return buildSlicePredicate(f)
	}

	// Scalar value
	return buildScalarPredicate(f)
}

// buildSlicePredicate builds a Predicate for a filter whose value is a slice or array.
//
//   - Single element → delegates to buildScalarPredicate (same output as a scalar).
//   - IN / = with multiple elements → field IN ($1, $2, …)
//   - NOT IN / != with multiple elements → field NOT IN ($1, $2, …)
//   - BETWEEN → field BETWEEN $1 AND $2 (requires exactly 2 time.Time values)
//   - Other operators with multiple elements → OR expansion via Group: (field op $1 OR field op $2)
func buildSlicePredicate(f *Filter) (criteria.Predicate, bool) {
	slice, ok := toAnySlice(f.Value)
	if !ok || len(slice) == 0 {
		return criteria.Predicate{}, false
	}

	// Single-element slice → treat as scalar.
	// IN/NOT IN have no scalar form; collapse to =/!= so the predicate is preserved
	// instead of being silently dropped by scalarConditionPredicate.
	if len(slice) == 1 {
		cond := f.Condition
		switch cond {
		case In:
			cond = Eq
		case NotIn:
			cond = NotEqual
		}
		return buildScalarPredicate(&Filter{Field: f.Field, Condition: cond, Value: slice[0]})
	}

	// BETWEEN with a []any containing time.Time values
	if f.Condition == Between {
		if times, ok := toTimeSlice(slice); ok && len(times) == 2 {
			return criteria.Between(f.Field, times[0], times[1]), true
		}
		logging.Error(errMsgBetweenRequiresPair, slog.String("field", f.Field))
		return criteria.Predicate{}, false
	}

	switch f.Condition {
	case In, Eq:
		return criteria.In(f.Field, slice), true
	case NotIn, NotEqual:
		return criteria.NotIn(f.Field, slice), true
	default:
		// Generic OR expansion: (field op $1 OR field op $2)
		parts := make([]criteria.Predicate, 0, len(slice)*2-1)
		for i, v := range slice {
			if i > 0 {
				parts = append(parts, criteria.Or())
			}
			p, ok := scalarConditionPredicate(f.Field, f.Condition, v)
			if !ok {
				return criteria.Predicate{}, false
			}
			parts = append(parts, p)
		}
		return criteria.Group(parts...), true
	}
}

// buildScalarPredicate builds a Predicate for a filter with a single scalar value.
// String values are normalized through resolveValueType; LIKE patterns receive %…% wrapping.
func buildScalarPredicate(f *Filter) (criteria.Predicate, bool) {
	switch v := resolveValueType(f.Value).(type) {
	case StringValue:
		switch f.Condition {
		case Contains:
			return criteria.Contains(f.Field, fmt.Sprintf("%%%s%%", v)), true
		case NotContains:
			return criteria.NotContains(f.Field, fmt.Sprintf("%%%s%%", v)), true
		case Like:
			return criteria.Like(f.Field, fmt.Sprintf("%%%s%%", v)), true
		case NotLike:
			return criteria.NotLike(f.Field, fmt.Sprintf("%%%s%%", v)), true
		default:
			return scalarConditionPredicate(f.Field, f.Condition, v)
		}

	case FloatValue, IntValue:
		return scalarConditionPredicate(f.Field, f.Condition, v)

	case []time.Time:
		if f.Condition == Between && len(v) == 2 {
			return criteria.Between(f.Field, v[0], v[1]), true
		}
		logging.Error(errMsgBetweenRequiresPair, slog.String("field", f.Field))
		return criteria.Predicate{}, false

	default:
		logging.Error(errMsgUnsupportedTypeField, slog.String("field", f.Field))
		return criteria.Predicate{}, false
	}
}

// scalarConditionPredicate maps a string operator to the corresponding criteria constructor.
func scalarConditionPredicate(field, condition string, value any) (criteria.Predicate, bool) {
	switch condition {
	case Eq:
		return criteria.Eq(field, value), true
	case NotEqual:
		return criteria.Ne(field, value), true
	case Less:
		return criteria.Lt(field, value), true
	case LessOrEqual:
		return criteria.Lte(field, value), true
	case Greater:
		return criteria.Gt(field, value), true
	case GreaterOrEqual:
		return criteria.Gte(field, value), true
	default:
		logging.Error(errMsgInvalidCondition, slog.String("condition", condition))
		return criteria.Predicate{}, false
	}
}

// Security

// ContainsNotAllowedValue reports whether a string contains SQL injection keywords.
// Uses word-boundary matching to avoid false positives (e.g. "EXECUTOR" does not match "EXEC").
// Secondary defense only — values are always bound as parameters, never interpolated.
func ContainsNotAllowedValue(input string) bool {
	for _, pattern := range notAllowedPatterns {
		if pattern.MatchString(input) {
			return true
		}
	}
	return false
}

// Internal Helpers

// resolveValueType normalises a raw value to one of the typed wrappers used
// by buildScalarPredicate. Strings containing "|" are treated as BETWEEN date ranges.
func resolveValueType(value any) any {
	switch v := value.(type) {
	case string:
		if strings.Contains(v, "|") {
			if times, err := parseBetweenDates(v); err == nil {
				return times
			}
		}
		return StringValue(v)
	case int:
		return IntValue(int64(v))
	case int32:
		return IntValue(int64(v))
	case int64:
		return IntValue(v)
	case float32:
		return FloatValue(float64(v))
	case float64:
		return FloatValue(v)
	default:
		return StringValue(fmt.Sprintf("%v", v))
	}
}

// parseBetweenDates parses a "startRFC3339|endRFC3339" string into a []time.Time pair.
func parseBetweenDates(value string) ([]time.Time, error) {
	parts := strings.Split(value, "|")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid BETWEEN format: expected two RFC3339 values separated by '|'")
	}
	start, err1 := time.Parse(timeLayout, parts[0])
	end, err2 := time.Parse(timeLayout, parts[1])
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("error parsing dates: %v, %v", err1, err2)
	}
	return []time.Time{start, end}, nil
}

// toAnySlice converts a slice or array to []any via reflection.
func toAnySlice(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	result := make([]any, rv.Len())
	for i := range rv.Len() {
		result[i] = rv.Index(i).Interface()
	}
	return result, true
}

// toTimeSlice converts []any to []time.Time, returning false if any element is not a time.Time.
func toTimeSlice(slice []any) ([]time.Time, bool) {
	times := make([]time.Time, 0, len(slice))
	for _, v := range slice {
		t, ok := v.(time.Time)
		if !ok {
			return nil, false
		}
		times = append(times, t)
	}
	return times, true
}

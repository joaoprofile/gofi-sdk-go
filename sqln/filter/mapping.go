package filter

import (
	"errors"
	"fmt"
	"slices"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// ErrInvalidFilter is wrapped by Build for every rejected filter or sort.
var ErrInvalidFilter = errors.New("sqln/filter: invalid filter")

// Mapping is the allowlist of a dynamic query: the names an API accepts,
// each bound to the column it filters. Fields outside the mapping are
// rejected, so clients cannot probe columns that are never returned (for
// example password_hash LIKE 'a%').
type Mapping map[string]Field

// Field describes one filterable name. Column is never serialized, so the
// mapping can be sent to a frontend to build filter screens.
type Field struct {
	// Column is the SQL column expression, e.g. "o.created_at".
	Column string `json:"-"`
	// Ops lists the accepted conditions; empty accepts every operator.
	Ops []string `json:"ops,omitempty"`
	// Sortable allows the name as a sort field.
	Sortable bool `json:"sortable,omitempty"`

	// UI metadata, passed through untouched.
	Label      string `json:"label,omitempty"`
	FilterType string `json:"filterType,omitempty"`
	SearchType string `json:"searchType,omitempty"`
	Content    any    `json:"content,omitempty"`
}

// Operator sets for Field.Ops.
var (
	Equality = []string{Eq, NotEqual, In, NotIn, IsNull, IsNotNull}
	Range    = []string{Eq, NotEqual, Less, LessOrEqual, Greater, GreaterOrEqual, Between, IsNull, IsNotNull}
	Text     = []string{Eq, NotEqual, In, NotIn, Contains, NotContains, Like, NotLike, IsNull, IsNotNull}
)

// Allow maps each column to itself with every operator and sorting enabled.
// It keeps the names clients already send while adding the allowlist; move
// to API names (Mapping{"created": {Column: "o.created_at"}}) when the
// contract can change.
func Allow(columns ...string) Mapping {
	m := make(Mapping, len(columns))
	for _, c := range columns {
		m[c] = Field{Column: c, Sortable: true}
	}
	return m
}

// Build appends the filters to base, which must end inside a WHERE clause,
// as "base AND ( … )". Placeholders continue after args, the parameters base
// already binds, and the returned Params are args followed by the filter
// values. Any filter outside m, or otherwise invalid, rejects the whole set.
// A nil dialect uses the active global connection. Requests above the
// bounds (DefaultMaxFilters, DefaultMaxInValues, DefaultMaxLikeLength, or
// opts) are rejected with ErrInvalidFilter.
func Build(base string, args []any, filters *Filters, m Mapping, dialect FilterDialect, opts ...Option) (*QueryParam, error) {
	if dialect == nil {
		dialect = activeDialect()
	}
	if filters == nil || len(filters.Filters) == 0 {
		return &QueryParam{Query: base, Params: args}, nil
	}
	lim := newLimits(opts)
	if n := countConditions(filters.Filters); n > lim.maxFilters {
		return nil, fmt.Errorf("%w: %d conditions exceed the limit of %d", ErrInvalidFilter, n, lim.maxFilters)
	}

	var errs []error
	if err := validateLogicalPlacement(filters.Filters); err != nil {
		errs = append(errs, fmt.Errorf("%w: %w", ErrInvalidFilter, err))
	}
	predicates := make([]criteria.Predicate, 0, len(filters.Filters))
	for i, f := range filters.Filters {
		p, err := m.checkedPredicate(f, lim)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w at %d: %w", ErrInvalidFilter, i, err))
			continue
		}
		predicates = append(predicates, p)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	clause, params, err := criteria.BuildClauseAfter(args, predicates, dialect)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
	}
	return &QueryParam{Query: base + " AND ( " + clause + " )", Params: params}, nil
}

// checkedPredicate enforces lim on a condition filter, then resolves it.
func (m Mapping) checkedPredicate(f *Filter, lim limits) (criteria.Predicate, error) {
	if f != nil && f.LogicalOperator == "" {
		if err := lim.check(f); err != nil {
			return criteria.Predicate{}, err
		}
	}
	return m.predicate(f)
}

// predicate resolves f through the mapping before building it.
func (m Mapping) predicate(f *Filter) (criteria.Predicate, error) {
	if f == nil {
		return criteria.Predicate{}, errors.New("nil filter")
	}
	if f.LogicalOperator != "" {
		p, ok := filterToPredicate(f)
		if !ok {
			return p, fmt.Errorf(errMsgInvalidLogicalOp, f.LogicalOperator)
		}
		return p, nil
	}
	field, ok := m[f.Field]
	if !ok {
		return criteria.Predicate{}, fmt.Errorf("field %q is not in the mapping", f.Field)
	}
	if !driver.IsIdentifier(field.Column) {
		return criteria.Predicate{}, fmt.Errorf("mapping for %q has an invalid column %q", f.Field, field.Column)
	}
	cond := f.Condition
	if f.Value == nil && cond == "" {
		cond = IsNull
	}
	if len(field.Ops) > 0 && !slices.Contains(field.Ops, cond) {
		return criteria.Predicate{}, fmt.Errorf("condition %q is not allowed for field %q", cond, f.Field)
	}
	resolved := *f
	resolved.Field = field.Column
	p, ok := filterToPredicate(&resolved)
	if !ok {
		return p, fmt.Errorf("field %q: invalid condition %q or value", f.Field, f.Condition)
	}
	return p, nil
}

// SortColumn resolves a sort field through the mapping; an empty field
// returns "" (the caller's default applies).
func (m Mapping) SortColumn(field string) (string, error) {
	if field == "" {
		return "", nil
	}
	f, ok := m[field]
	if !ok || !f.Sortable {
		return "", fmt.Errorf("%w: "+errMsgInvalidSortingField, ErrInvalidFilter, field)
	}
	if !driver.IsIdentifier(f.Column) {
		return "", fmt.Errorf("%w: mapping for %q has an invalid column %q", ErrInvalidFilter, field, f.Column)
	}
	return f.Column, nil
}

// validateLogicalPlacement rejects leading, trailing and consecutive AND/OR.
func validateLogicalPlacement(filters []*Filter) error {
	if len(filters) == 0 {
		return nil
	}
	if filters[0] != nil && filters[0].LogicalOperator != "" {
		return errors.New(errMsgLeadingLogicalOp)
	}
	if last := filters[len(filters)-1]; last != nil && last.LogicalOperator != "" {
		return errors.New(errMsgTrailingLogicalOp)
	}
	for i := 1; i < len(filters); i++ {
		if filters[i] != nil && filters[i-1] != nil && filters[i].LogicalOperator != "" && filters[i-1].LogicalOperator != "" {
			return errors.New(errMsgConsecutiveLogicalOp)
		}
	}
	return nil
}

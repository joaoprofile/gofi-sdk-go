package sqln

import (
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver"
	"github.com/gofi-labs/gofi-sdk-go/sqln/filter"
)

// Types

type Filter = filter.Filter
type FilterParams = filter.FilterParams
type Filters = filter.Filters
type QueryParam = filter.QueryParam

// FilterDialect is the SQL generation interface required by the filter engine.
// Sourced from the driver package — the single authoritative definition.
type FilterDialect = driver.FilterDialect

// FilterMapping is the allowlist of names a dynamic query accepts, each bound
// to its column; FilterField describes one name (see filter.Mapping).
type FilterMapping = filter.Mapping
type FilterField = filter.Field

// Operator sets for FilterField.Ops.
var (
	Equality = filter.Equality
	Range    = filter.Range
	Text     = filter.Text
)

// AllowColumns maps each column to itself (see filter.Allow).
func AllowColumns(columns ...string) FilterMapping { return filter.Allow(columns...) }

// Value type wrappers for type-safe parameter binding in tests and custom scan logic.
type StringValue = filter.StringValue
type FloatValue = filter.FloatValue
type IntValue = filter.IntValue

// Comparison Operators

const (
	Eq             = driver.Eq
	NotEqual       = driver.NotEqual
	Less           = driver.Less
	LessOrEqual    = driver.LessOrEqual
	Greater        = driver.Greater
	GreaterOrEqual = driver.GreaterOrEqual
)

// Membership Operators

const (
	In    = driver.In
	NotIn = driver.NotIn
)

// Text Search Operators

const (
	// Contains performs a case-insensitive substring match via the active dialect.
	// PostgreSQL: ILIKE   MySQL / SQL Server / Oracle: LIKE
	Contains = driver.Contains

	// NotContains performs a case-insensitive negative substring match via the active dialect.
	// PostgreSQL: NOT ILIKE   Others: NOT LIKE
	NotContains = driver.NotContains

	// Like performs a literal case-sensitive LIKE on all databases.
	Like = driver.Like

	// NotLike performs a literal case-sensitive NOT LIKE on all databases.
	NotLike = driver.NotLike
)

// Range Operator

const Between = driver.Between

// Null Check Operators

const (
	IsNull    = driver.IsNull
	IsNotNull = driver.IsNotNull
)

//  Logical Operators

const (
	Or  = driver.Or
	And = driver.And
)

//  Constructors

func NewFilter(field, condition string, value any) *Filter {
	return filter.NewFilter(field, condition, value)
}

func AND() *Filter { return filter.AND() }
func OR() *Filter  { return filter.OR() }

func NewFilters() *Filters {
	return filter.NewFilters()
}

// FilterOption changes the bounds BuildQuery enforces (see filter.Option).
type FilterOption = filter.Option

// BuildQuery appends the filters to base through the mapping; placeholders
// continue after args. A nil dialect uses the active connection. Requests
// above filter.DefaultMaxFilters conditions, DefaultMaxInValues list values
// or DefaultMaxLikeLength LIKE characters are rejected (see filter.Build).
func BuildQuery(base string, args []any, f *Filters, m FilterMapping, dialect FilterDialect, opts ...FilterOption) (*QueryParam, error) {
	return filter.Build(base, args, f, m, dialect, opts...)
}

// ErrInvalidFilter is wrapped by BuildQuery for each rejected filter or sort.
var ErrInvalidFilter = filter.ErrInvalidFilter

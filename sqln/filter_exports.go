package sqln

import (
	"github.com/joaoprofile/gofi/sqln/driver"
	"github.com/joaoprofile/gofi/sqln/filter"
)

// Types

type Filter = filter.Filter
type FilterParams = filter.FilterParams
type Filters = filter.Filters
type QueryParam = filter.QueryParam

// FilterDialect is the SQL generation interface required by the filter engine.
// Sourced from the driver package — the single authoritative definition.
type FilterDialect = driver.FilterDialect

// FieldMapping and QueryMapping allow consumers to define allowed fields and operators
// for dynamic query validation without importing the filter sub-package directly.
type FieldMapping = filter.FieldMapping
type QueryMapping = filter.QueryMapping

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

// NewQueryBuild appends filter conditions to the base query using PostgreSQL-style
// parameterized placeholders ($1, $2, …). For other databases, use NewQueryBuildWithDialect.
func NewQueryBuild(query string, f *Filters) *QueryParam {
	return filter.NewQueryBuild(query, f)
}

// NewQueryBuildWithDialect appends filter conditions using the given database dialect
// for placeholder style and case-insensitive LIKE behavior.
func NewQueryBuildWithDialect(query string, f *Filters, dialect FilterDialect) *QueryParam {
	return filter.NewQueryBuildWithDialect(query, f, dialect)
}

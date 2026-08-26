package driver

// Comparison Operators

// Comparison operators are valid for all databases.
const (
	Eq             = "="
	NotEqual       = "!="
	Less           = "<"
	LessOrEqual    = "<="
	Greater        = ">"
	GreaterOrEqual = ">="
)

// Membership operators test whether a value belongs to a set.
const (
	In    = "IN"
	NotIn = "NOT IN"
)

// Text Search Operators

const (
	Contains    = "LIKE"     // case-insensitive via dialect: ILIKE (PG) / LIKE (others)
	NotContains = "NOT LIKE" // case-insensitive NOT via dialect: NOT ILIKE (PG) / NOT LIKE (others)
	Like        = "~~"       // literal case-sensitive LIKE, all databases
	NotLike     = "!~~"      // literal case-sensitive NOT LIKE, all databases
)

// Range Operator

// Between accepts:
//   - a "startRFC3339|endRFC3339" string
//   - a []time.Time{start, end}
//   - a []any{start, end} where elements are time.Time
const Between = "BETWEEN"

// Null Check Operators─
// Null check operators do not require a bound value.
const (
	IsNull    = "IS NULL"
	IsNotNull = "IS NOT NULL"
)

// Boolean Check Operators
// Boolean check operators do not require a bound value.
const (
	IsTrue  = "IS TRUE"
	IsFalse = "IS FALSE"
)

// Group Operator — internal sentinel used by Group predicate.
// Never emitted verbatim; the builder wraps the inner clause in parentheses.
const Group = "__group__"

// Logical Operators

// Logical operators separate predicates within a filter list.
const (
	And = "AND"
	Or  = "OR"
)

// Operator Allowlist

// AllowedOperators is the authoritative set of valid filter operator strings.
// Used by the filter and criteria engines to reject unknown or injected operators.
var AllowedOperators = map[string]struct{}{
	Eq: {}, NotEqual: {}, Less: {}, LessOrEqual: {}, Greater: {}, GreaterOrEqual: {},
	In: {}, NotIn: {},
	Contains: {}, NotContains: {},
	Like: {}, NotLike: {},
	Between: {},
	IsNull:  {}, IsNotNull: {},
	IsTrue: {}, IsFalse: {},
	Group: {},
}

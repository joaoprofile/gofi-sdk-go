package criteria

const (
	ASC  = "ASC"
	DESC = "DESC"
)

// Order defines a sort expression for a query (field + direction).
//
// Field must be a column reference (see driver.IsIdentifier), so it may come
// from a request; anything else fails Build with ErrInvalidField. Mark a
// trusted SQL expression with Raw.
type Order struct {
	Field     string
	Direction string
	raw       bool // Field is a trusted expression, written without validation
}

// Raw marks Field as a trusted SQL expression (e.g. COUNT(*)) written as is.
// Trusted input only — never user data.
func (o Order) Raw() Order {
	o.raw = true
	return o
}

// Asc creates an ascending ORDER BY expression.
func Asc(field string) Order { return Order{Field: field, Direction: ASC} }

// Desc creates a descending ORDER BY expression.
func Desc(field string) Order { return Order{Field: field, Direction: DESC} }

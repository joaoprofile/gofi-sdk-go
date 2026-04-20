package criteria

const (
	ASC  = "ASC"
	DESC = "DESC"
)

// Order defines a sort expression for a query (field + direction).
type Order struct {
	Field     string
	Direction string
}

// Asc creates an ascending ORDER BY expression.
func Asc(field string) Order { return Order{Field: field, Direction: ASC} }

// Desc creates a descending ORDER BY expression.
func Desc(field string) Order { return Order{Field: field, Direction: DESC} }

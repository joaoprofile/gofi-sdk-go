package pagination

type SortDirection string

const (
	ASC  SortDirection = "ASC"
	DESC SortDirection = "DESC"
)

type Sort struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

// NewSort builds a sort entry. field is written into ORDER BY and is dropped
// by GetOrder unless it is a plain column reference (driver.IsIdentifier);
// resolve request values through an allowlist such as filter.Mapping.SortColumn.
func NewSort(field string, direction SortDirection) Sort {
	return Sort{field, direction}
}

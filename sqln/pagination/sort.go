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

func NewSort(field string, direction SortDirection) Sort {
	return Sort{field, direction}
}

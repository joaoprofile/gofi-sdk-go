package pagination

type Page[T any] struct {
	TotalPages       uint64 `json:"totalPages"`
	TotalElements    uint64 `json:"totalElements"`
	Size             uint64 `json:"size"`
	Number           uint64 `json:"number"`
	NumberOfElements uint64 `json:"numberOfElements"`
	Content          []T    `json:"content"`
}

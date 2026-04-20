package pagination

import (
	"fmt"
	"strings"
)

const (
	DefaultPage          = uint16(0)
	DefaultLimit         = uint16(15)
	DefaultSortField     = "id"
	DefaultSortDirection = string(ASC)
)

type PageRequest struct {
	Page  uint16
	Limit uint16
	Order []Sort
}

func NewPageRequest(page uint16, limit uint16, order []Sort) *PageRequest {
	return &PageRequest{page, limit, order}
}

func (p *PageRequest) GetOrder() string {
	orders := make([]string, 0, len(p.Order))
	for _, order := range p.Order {
		orders = append(orders, fmt.Sprintf("%s %s", order.Field, order.Direction))
	}
	return strings.Join(orders, ", ")
}

// NewPageRequestFromParams builds a PageRequest from explicit pagination parameters.
// This is the coupling-free alternative to NewPageRequestFilter (which lives in the sqln root).
func NewPageRequestFromParams(page, limit uint16, sortField, sortDirection string) *PageRequest {
	if page == 0 {
		page = DefaultPage
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	if sortField == "" {
		sortField = DefaultSortField
	}
	if sortDirection == "" {
		sortDirection = DefaultSortDirection
	}
	order := []Sort{NewSort(sortField, SortDirection(sortDirection))}
	return NewPageRequest(page, limit, order)
}

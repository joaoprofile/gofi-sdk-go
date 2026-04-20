package sqln

import (
	"github.com/joaoprofile/gofi/sqln/filter"
	"github.com/joaoprofile/gofi/sqln/pagination"
)

type Sort = pagination.Sort

type Page[T any] = pagination.Page[T]

type PageRequest = pagination.PageRequest

func NewSort(field string, direction SortDirection) Sort {
	return pagination.NewSort(field, direction)
}

func NewPageRequest(page uint16, limit uint16, order []Sort) *PageRequest {
	if limit == 0 {
		limit = DefaultLimit
	}

	return pagination.NewPageRequest(page, limit, order)
}

func NewPageRequestFilter(f *filter.Filters) *PageRequest {
	var page, limit uint16
	var sortField, sortDirection string

	if f != nil && f.Params != nil {
		page = f.Params.Page
		limit = f.Params.Limit
		sortField = f.Params.SortField
		sortDirection = f.Params.SortDirection
	}

	return pagination.NewPageRequestFromParams(page, limit, sortField, sortDirection)
}

package sqln

import (
	"github.com/gofi-labs/gofi-sdk-go/sqln/filter"
	"github.com/gofi-labs/gofi-sdk-go/sqln/pagination"
)

type Sort = pagination.Sort

type Page[T any] = pagination.Page[T]

type PageRequest = pagination.PageRequest

// NewSort builds a sort entry; see pagination.NewSort for how field is validated.
func NewSort(field string, direction SortDirection) Sort {
	return pagination.NewSort(field, direction)
}

// PageOption configures the page size bound (see WithMaxLimit).
type PageOption = pagination.Option

// DefaultMaxLimit caps the page size of NewPageRequest and NewPageRequestFilter.
const DefaultMaxLimit = pagination.DefaultMaxLimit

// WithMaxLimit replaces DefaultMaxLimit, e.g. for internal batch jobs.
func WithMaxLimit(n uint16) PageOption { return pagination.WithMaxLimit(n) }

// NewPageRequest builds a page; limit is capped at DefaultMaxLimit (see
// WithMaxLimit). Order fields that are not column references are dropped
// from ORDER BY (see pagination.PageRequest.GetOrder).
func NewPageRequest(page uint16, limit uint16, order []Sort, opts ...PageOption) *PageRequest {
	return pagination.NewPageRequest(page, pagination.ClampLimit(limit, opts...), order)
}

// NewPageRequestFilter builds the page from the request parameters; the sort
// field is resolved through m and must be Sortable, and the limit is capped
// at DefaultMaxLimit (see WithMaxLimit).
func NewPageRequestFilter(f *filter.Filters, m filter.Mapping, opts ...PageOption) (*PageRequest, error) {
	var page, limit uint16
	var sortField, sortDirection string

	if f != nil && f.Params != nil {
		page = f.Params.Page
		limit = f.Params.Limit
		sortDirection = f.Params.SortDirection
		column, err := m.SortColumn(f.Params.SortField)
		if err != nil {
			return nil, err
		}
		sortField = column
	}

	return pagination.NewPageRequestFromParams(page, limit, sortField, sortDirection, opts...), nil
}

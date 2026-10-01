package pagination

import (
	"log/slog"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/sqln/driver"
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

// GetOrder renders the ORDER BY list; entries whose field is not a column reference are dropped.
func (p *PageRequest) GetOrder() string {
	orders := make([]string, 0, len(p.Order))
	for _, order := range p.Order {
		if !driver.IsIdentifier(order.Field) {
			slog.Warn("sqln: sort field dropped, not a column reference", slog.String("field", order.Field))
			continue
		}
		orders = append(orders, order.Field+" "+driver.SortDirection(string(order.Direction)))
	}
	return strings.Join(orders, ", ")
}

// DefaultMaxLimit bounds the page size of requests built from client
// parameters, so a client cannot ask for 65535 rows per page.
const DefaultMaxLimit = uint16(100)

// Option configures how a PageRequest is built from client parameters.
type Option func(*options)

type options struct{ maxLimit uint16 }

// WithMaxLimit replaces DefaultMaxLimit; 0 keeps the default.
func WithMaxLimit(n uint16) Option { return func(o *options) { o.maxLimit = n } }

// ClampLimit returns limit, DefaultLimit when zero, capped at the maximum.
func ClampLimit(limit uint16, opts ...Option) uint16 {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if o.maxLimit == 0 {
		o.maxLimit = DefaultMaxLimit
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	return min(limit, o.maxLimit)
}

// NewPageRequestFromParams builds a PageRequest from explicit pagination parameters.
// This is the coupling-free alternative to NewPageRequestFilter (which lives in the sqln root).
// limit is capped at DefaultMaxLimit (see WithMaxLimit).
// sortField is written into ORDER BY: GetOrder drops it unless it is a plain
// column reference (driver.IsIdentifier). Prefer resolving request sort names
// through an allowlist (filter.Mapping.SortColumn) so clients cannot sort by,
// and so probe, columns they never see.
func NewPageRequestFromParams(page, limit uint16, sortField, sortDirection string, opts ...Option) *PageRequest {
	if page == 0 {
		page = DefaultPage
	}
	limit = ClampLimit(limit, opts...)
	if sortField == "" {
		sortField = DefaultSortField
	}
	if sortDirection == "" {
		sortDirection = DefaultSortDirection
	}
	order := []Sort{NewSort(sortField, SortDirection(sortDirection))}
	return NewPageRequest(page, limit, order)
}

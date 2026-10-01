// Package product serves a catalog search whose filters come from the client.
package product

import (
	"context"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/sqln"
)

type Product struct {
	ID        int64     `db:"id"         json:"id"`
	Name      string    `db:"name"       json:"name"`
	Price     float64   `db:"price"      json:"price"`
	Category  string    `db:"category"   json:"category"`
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
}

// Fields is the allowlist of the dynamic search: the names a client may
// filter or sort by, each bound to its column and to the operators it accepts.
// Anything else is rejected with sqln.ErrInvalidFilter. Column is never
// serialized, so the same map is returned by GET /products/filters.
var Fields = sqln.FilterMapping{
	"name":     {Column: "p.name", Ops: sqln.Text, Sortable: true, Label: "Name"},
	"category": {Column: "c.slug", Ops: sqln.Equality, Label: "Category"},
	"price":    {Column: "p.price", Ops: sqln.Range, Sortable: true, Label: "Price"},
	"created":  {Column: "p.created_at", Ops: sqln.Range, Sortable: true, Label: "Created at"},
}

// baseQuery ends inside the WHERE clause: the client filters are appended as
// "AND ( ... )", with placeholders numbered after the base arguments.
const baseQuery = `SELECT p.id, p.name, p.price, c.slug AS category, p.created_at
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.active IS TRUE`

// Repository uses the global connection registered by gofi's AddDatabase.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

// Search applies the client filters, sorting and pagination.
func (r *Repository) Search(ctx context.Context, f *sqln.Filters) (*sqln.Page[Product], error) {
	q, err := sqln.BuildQuery(baseQuery, nil, f, Fields, nil)
	if err != nil {
		return nil, err
	}
	page, err := sqln.NewPageRequestFilter(f, Fields)
	if err != nil {
		return nil, err
	}
	return sqln.FindWithFilter[Product](ctx, q).WithPage(page).PagedList()
}

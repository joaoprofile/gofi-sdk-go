// Package product reads the catalog with sqln criteria queries.
package product

import (
	"context"
	"iter"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
)

// Product is filled by the db tags; the column order in Select does not matter.
type Product struct {
	ID       int64   `db:"id"`
	Name     string  `db:"name"`
	Price    float64 `db:"price"`
	Category string  `db:"category"`
}

// Filter holds the optional search inputs: zero values are ignored.
type Filter struct {
	Term       string   // matches the product name or the category slug
	Categories []string // category slugs
	MaxPrice   float64
}

// Repository uses the global connection registered by gofi's AddDatabase.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

// catalog is the base of every query: active products joined with their category.
func catalog() *criteria.Query {
	return criteria.From("products", "p").
		Select("p.id", "p.name", "p.price", "c.slug AS category").
		Join("categories", "c", "c.id = p.category_id")
}

// Search builds the WHERE clause only from the filters that were set and
// returns one page plus the total count.
func (r *Repository) Search(ctx context.Context, f Filter, page *sqln.PageRequest) (*sqln.Page[Product], error) {
	where := []criteria.Predicate{criteria.IsTrue("p.active")}
	if f.Term != "" {
		// (p.name ILIKE $n OR c.slug = $m)
		where = append(where, criteria.Group(
			criteria.Contains("p.name", "%"+f.Term+"%"),
			criteria.Or(),
			criteria.Eq("c.slug", f.Term),
		))
	}
	if len(f.Categories) > 0 {
		where = append(where, criteria.In("c.slug", f.Categories))
	}
	if f.MaxPrice > 0 {
		where = append(where, criteria.Lte("p.price", f.MaxPrice))
	}

	return sqln.FindFromCriteria[Product](ctx, catalog().Where(where...)).WithPage(page).PagedList()
}

// FindByID returns nil when the product does not exist.
func (r *Repository) FindByID(ctx context.Context, id int64) (*Product, error) {
	return sqln.FindFromCriteria[Product](ctx, catalog().Where(criteria.Eq("p.id", id))).UniqueResult()
}

// All streams every active product, one row at a time, without building a slice.
func (r *Repository) All(ctx context.Context) iter.Seq2[Product, error] {
	return sqln.FindFromCriteria[Product](ctx, catalog().Where(criteria.IsTrue("p.active"))).All()
}

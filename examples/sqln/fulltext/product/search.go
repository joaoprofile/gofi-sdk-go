// Package product searches the catalog with sqln full-text queries.
package product

import (
	"context"
	"errors"

	"github.com/joaoprofile/gofi-sdk-go/sqln"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
	"github.com/joaoprofile/gofi-sdk-go/sqln/pagination"
)

//go:generate go test -run TestMigrationIsCurrent -update .

// searchIndex is the full-text index of the catalog. Declared at package
// level with MustIndex, it is registered: gofi's database component checks
// it when the service starts. The migration in .migrations is rendered from
// it (see migration_test.go), and every query below reads it.
var searchIndex = fts.MustIndex("products", fts.Spec{
	Name:     "ftx_products_search",
	Columns:  []fts.Column{{Name: "name", Weight: fts.A}, {Name: "description", Weight: fts.B}},
	Language: fts.Portuguese,
	Unaccent: true, // "cafe" finds "café"
})

// Hit is a matched product and its relevance.
type Hit struct {
	ID        int64   `db:"id"`
	Name      string  `db:"name"`
	Relevance float64 `db:"relevance"`
}

// Repository uses the global connection registered by gofi's AddDatabase.
type Repository struct{}

func NewRepository() *Repository { return &Repository{} }

// Find returns one page of active products matching term, most relevant
// first. An empty search (only punctuation or stopwords) is an empty page,
// not an error.
func (r *Repository) Find(ctx context.Context, term string, mode fts.Mode, page, size uint16) (*sqln.Page[Hit], error) {
	s := searchIndex.Search(term).Mode(mode)
	q := criteria.From("products", "p").
		Select("p.id", "p.name").
		SelectRelevance(s, "relevance").
		Where(criteria.IsTrue("p.active"), criteria.Match(s))

	order := []pagination.Sort{pagination.NewSort("relevance", pagination.DESC), pagination.NewSort("p.id", pagination.ASC)}
	res, err := sqln.FindFromCriteria[Hit](ctx, q).WithPage(pagination.NewPageRequest(page, size, order)).PagedList()
	if errors.Is(err, fts.ErrEmptySearch) {
		return &sqln.Page[Hit]{}, nil
	}
	return res, err
}

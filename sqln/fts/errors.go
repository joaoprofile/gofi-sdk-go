// Package fts defines full-text indexes and the searches that read them.
//
// An Index is the single definition of a full-text index: the migration that
// creates it and every query that searches it are rendered from it, so the
// searched expression (PostgreSQL) or column list (MySQL) is always the one
// the index was built on. There is no full-text API taking a column name: a
// search exists only on an Index, and before a search runs, the connection
// checks that the index exists in the database as defined (see Verify).
//
//	var ProductSearch = fts.MustIndex("products", fts.Spec{
//	    Name:     "ftx_products_search",
//	    Columns:  []fts.Column{{Name: "name", Weight: fts.A}, {Name: "description", Weight: fts.B}},
//	    Language: fts.Portuguese,
//	    Unaccent: true,
//	})
//
//	criteria.From("products", "p").
//	    Where(criteria.Match(ProductSearch.Search(term).Mode(fts.Prefix)))
package fts

import "errors"

var (
	// ErrInvalidIndex reports an Index definition that cannot be built.
	ErrInvalidIndex = errors.New("fts: invalid index")
	// ErrInvalidSearch reports a search above the length or term bounds.
	ErrInvalidSearch = errors.New("fts: invalid search")
	// ErrEmptySearch reports a search with no searchable word left, e.g. only
	// punctuation, stopwords or words shorter than the MySQL token size.
	// Callers usually skip the search filter or return an empty result.
	ErrEmptySearch = errors.New("fts: empty search")
	// ErrUnsupported reports a dialect without full-text search.
	ErrUnsupported = errors.New("fts: full-text search not supported by the dialect")
	// ErrIndexMissing reports an index absent from the database: apply the
	// migration rendered by Migration.
	ErrIndexMissing = errors.New("fts: full-text index missing")
	// ErrIndexOutdated reports an index created from a different definition:
	// the Index changed and its migration was not applied.
	ErrIndexOutdated = errors.New("fts: full-text index outdated")
	// ErrServerConfig reports a server setting the index depends on, such as
	// MySQL's innodb_ft_min_token_size, differing from the definition.
	ErrServerConfig = errors.New("fts: server configuration does not match the index")
)

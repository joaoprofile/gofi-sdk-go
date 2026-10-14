package criteria

import (
	"fmt"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

// Full-text search

// Match produces the full-text condition of s on its index:
//   - PostgreSQL → p.<vector> @@ to_tsquery('<config>'::regconfig, $n)
//   - MySQL      → MATCH(p.col1, p.col2) AGAINST (? IN BOOLEAN MODE)
//
// The indexed columns are qualified by the alias of the index's table in the
// query (see fts.Search.On). The search string is bound as a parameter. Build
// fails with ErrInvalidValue wrapping fts.ErrEmptySearch when no searchable
// word remains, and fts.ErrUnsupported on dialects without full-text search.
// Queries run through the sqln manager check the index exists first.
func Match(s fts.Search) Predicate {
	return Predicate{operator: driver.Search, search: &s}
}

// ByRelevance orders by the relevance of s on its index, most relevant first.
func ByRelevance(s fts.Search) Order {
	return Order{Direction: DESC, search: &s}
}

// SelectRelevance adds the relevance of s to the SELECT list as alias, so a
// paged query can sort by it (pagination.NewSort(alias, pagination.DESC)).
// alias must be a column reference.
func (q *Query) SelectRelevance(s fts.Search, alias string) *Query {
	q.relevance = append(q.relevance, relevanceSelect{search: s, alias: alias})
	return q
}

type relevanceSelect struct {
	search fts.Search
	alias  string
}

// TextIndexes lists the full-text indexes q searches, so the caller can check
// they exist before running it (the sqln manager does).
func (q *Query) TextIndexes() []*fts.Index {
	var out []*fts.Index
	for _, r := range q.relevance {
		out = appendIndex(out, r.search.Index())
	}
	out = appendTextIndexes(out, q.where)
	out = appendTextIndexes(out, q.having)
	for _, o := range q.order {
		if o.search != nil {
			out = appendIndex(out, o.search.Index())
		}
	}
	return out
}

// TextIndexes lists the full-text indexes the predicates search.
func TextIndexes(predicates []Predicate) []*fts.Index {
	return appendTextIndexes(nil, predicates)
}

func appendTextIndexes(out []*fts.Index, predicates []Predicate) []*fts.Index {
	for _, p := range predicates {
		if p.search != nil {
			out = appendIndex(out, p.search.Index())
		}
		out = appendTextIndexes(out, p.children)
	}
	return out
}

func appendIndex(out []*fts.Index, idx *fts.Index) []*fts.Index {
	if idx == nil {
		return out
	}
	for _, have := range out {
		if have == idx {
			return out
		}
	}
	return append(out, idx)
}

// textExpr binds the search string of s and renders its match condition, or
// its relevance when rank is set.
func (b *builder) textExpr(s *fts.Search, rank bool) string {
	fd, ok := b.dialect.(driver.FullTextDialect)
	if !ok {
		b.fail(fmt.Errorf("%w: %w: %T", ErrInvalidValue, fts.ErrUnsupported, b.dialect))
		return ""
	}
	value, err := s.Value(b.dialect)
	if err != nil {
		b.fail(fmt.Errorf("%w: %w", ErrInvalidValue, err))
		return ""
	}
	qualifier, ok := b.qualifier(s)
	if !ok {
		return ""
	}
	def := s.Index().Definition()
	param := b.nextParam(value)
	if rank {
		return fd.TextRank(def, qualifier, param)
	}
	return fd.TextMatch(def, qualifier, param)
}

// qualifier is the alias set by Search.On, else the alias of the index's
// table among the query's FROM and JOIN tables. A bare clause (BuildClause)
// has no tables: its columns stay unqualified.
func (b *builder) qualifier(s *fts.Search) (string, bool) {
	if q := s.Qualifier(); q != "" {
		if !driver.IsIdentifier(q) {
			b.fail(fmt.Errorf("%w: search qualifier %q is not an alias", ErrInvalidField, q))
			return "", false
		}
		return q, true
	}
	if b.tables == nil {
		return "", true
	}
	idx := s.Index()
	switch qs := b.tables[idx.Table()]; len(qs) {
	case 1:
		return qs[0], true
	case 0:
		b.fail(fmt.Errorf("%w: table %s of full-text index %s is not in the query; set Search.On", ErrInvalidField, idx.Table(), idx.Name()))
	default:
		b.fail(fmt.Errorf("%w: table %s of full-text index %s appears %d times; set Search.On", ErrInvalidField, idx.Table(), idx.Name(), len(qs)))
	}
	return "", false
}

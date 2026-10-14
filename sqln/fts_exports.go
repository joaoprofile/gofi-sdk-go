package sqln

import (
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

// FullTextIndex is a full-text index definition; see the fts package.
type FullTextIndex = fts.Index

// FullTextSearch is a search string on a FullTextIndex.
type FullTextSearch = fts.Search

// Match produces the full-text condition of s on its index (see criteria.Match).
func Match(s FullTextSearch) Predicate { return criteria.Match(s) }

// ByRelevance orders a criteria query by the relevance of s, most relevant first.
func ByRelevance(s FullTextSearch) CriteriaOrder { return criteria.ByRelevance(s) }

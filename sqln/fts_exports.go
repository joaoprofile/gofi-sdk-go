package sqln

import (
	"context"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
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

// SuggestSearch corrects the misspelled words of s on the global connection
// (read replica when configured); see fts.Suggest. A typical search shows
// "did you mean" when s finds nothing and the corrected search does.
func SuggestSearch(ctx context.Context, s FullTextSearch) (FullTextSearch, bool, error) {
	conn, err := connection.Global()
	if err != nil {
		return s, false, err
	}
	ctx, cancel := conn.WithQueryTimeout(ctx)
	defer cancel()
	return fts.Suggest(ctx, conn.ReadDB(), conn.Dialect(), s)
}

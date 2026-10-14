package fts

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Lexicon bounds: shorter words are not corrected nor suggested, longer
// ones do not fit the lexicon.
const (
	minLexiconWord = 3
	maxLexiconWord = 64
	// SuggestThreshold is the least trigram similarity of a suggestion,
	// pg_trgm's default similarity_threshold.
	SuggestThreshold = 0.3
)

// RefreshLexicon rebuilds the lexicon of idx (Spec.Suggest) from the indexed
// columns: every word of 3 to 64 characters, keyed without accents, with the
// number of rows holding it. It reads the whole table, so run it from a job,
// after bulk loads or periodically, not per request. Both databases get the
// same lexicon: the words are split by the same rules as searches.
func RefreshLexicon(ctx context.Context, db *sql.DB, d driver.Dialect, idx *Index) (int, error) {
	if idx == nil || !idx.def.Suggest {
		return 0, fmt.Errorf("%w: RefreshLexicon needs an index with Suggest", ErrInvalidIndex)
	}
	if _, err := fullText(d); err != nil {
		return 0, err
	}
	entries, err := collectWords(ctx, db, &idx.def)
	if err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	lex := idx.def.LexiconTable()
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+lex); err != nil { // #nosec G202 -- name derived from a validated index
		return 0, fmt.Errorf("fts: clearing %s: %w", lex, err)
	}
	const batch = 500
	for start := 0; start < len(entries); start += batch {
		chunk := entries[start:min(start+batch, len(entries))]
		var sb strings.Builder
		args := make([]any, 0, 3*len(chunk))
		sb.WriteString("INSERT INTO " + lex + " (term, word, ndoc) VALUES ")
		for i, e := range chunk {
			if i > 0 {
				sb.WriteString(", ")
			}
			args = append(args, e.term, e.word, e.ndoc)
			n := len(args)
			sb.WriteString("(" + d.Param(n-2) + ", " + d.Param(n-1) + ", " + d.Param(n) + ")")
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return 0, fmt.Errorf("fts: filling %s: %w", lex, err)
		}
	}
	return len(entries), tx.Commit()
}

type lexiconEntry struct {
	term, word string
	ndoc       int
}

// collectWords reads the indexed columns and counts, per folded term, the
// rows holding it and its most frequent spelling.
func collectWords(ctx context.Context, db *sql.DB, def *driver.TextIndex) ([]lexiconEntry, error) {
	cols := make([]string, len(def.Columns))
	for i, c := range def.Columns {
		cols[i] = c.Name
	}
	// Table and columns are identifiers NewIndex validated, never input.
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM "+def.Table) // #nosec G202 -- validated identifiers
	if err != nil {
		return nil, fmt.Errorf("fts: reading %s: %w", def.Table, err)
	}
	defer rows.Close()

	ndoc := map[string]int{}
	spellings := map[string]map[string]int{}
	values := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, v := range values {
			for _, w := range splitWords(v.String) {
				if n := utf8.RuneCountInString(w); n < minLexiconWord || n > maxLexiconWord {
					continue
				}
				term := driver.FoldAccents(w)
				if spellings[term] == nil {
					spellings[term] = map[string]int{}
				}
				spellings[term][w]++
				if !seen[term] {
					seen[term] = true
					ndoc[term]++
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]lexiconEntry, 0, len(ndoc))
	for term, n := range ndoc {
		out = append(out, lexiconEntry{term: term, word: mostFrequent(spellings[term]), ndoc: n})
	}
	slices.SortFunc(out, func(a, b lexiconEntry) int { return strings.Compare(a.term, b.term) })
	return out, nil
}

func mostFrequent(counts map[string]int) string {
	best, bestN := "", 0
	for w, n := range counts {
		if n > bestN || n == bestN && w < best {
			best, bestN = w, n
		}
	}
	return best
}

// Suggest corrects the words of s missing from the lexicon of its index
// (Spec.Suggest) with the most similar indexed word, and reports whether it
// changed any. Show the corrected Search's Term ("did you mean …") and run
// it, typically when s found nothing. Only positive, whole words of 3 or more
// characters are corrected; the rest of the search string is kept as typed.
// The database proposes candidates through the lexicon's similarity index
// (pg_trgm on PostgreSQL, ngram on MySQL) and the choice is the same on
// both: the highest trigram similarity, as pg_trgm computes it, of at least
// SuggestThreshold, then the most frequent word.
func Suggest(ctx context.Context, db Querier, d driver.Dialect, s Search) (Search, bool, error) {
	if s.index == nil || !s.index.def.Suggest {
		return s, false, fmt.Errorf("%w: Suggest needs an index with Suggest", ErrInvalidIndex)
	}
	fd, err := fullText(d)
	if err != nil {
		return s, false, err
	}
	q, err := s.Query()
	if err != nil {
		if err == ErrEmptySearch {
			return s, false, nil
		}
		return s, false, err
	}
	keys := correctable(q)
	if len(keys) == 0 {
		return s, false, nil
	}
	known, err := knownTerms(ctx, db, d, &s.index.def, keys)
	if err != nil {
		return s, false, err
	}
	fix := map[string]string{}
	for _, key := range keys {
		if known[key] {
			continue
		}
		var args []any
		query := fd.TextCandidates(&s.index.def, key, func(v any) string {
			args = append(args, v)
			return d.Param(len(args))
		})
		word, err := bestCandidate(ctx, db, query, args, key)
		if err != nil {
			return s, false, err
		}
		if word != "" {
			fix[key] = word
		}
	}
	if len(fix) == 0 {
		return s, false, nil
	}
	s.term = replaceWords(s.term, fix)
	return s, true, nil
}

// Term returns the search string.
func (s Search) Term() string { return s.term }

// correctable lists the folded positive whole words of q worth correcting.
func correctable(q driver.TextQuery) []string {
	var keys []string
	for _, g := range q {
		for _, t := range g {
			if t.Negate || t.Prefix {
				continue
			}
			for _, w := range t.Words {
				if utf8.RuneCountInString(w) >= minLexiconWord {
					keys = append(keys, driver.FoldAccents(w))
				}
			}
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func knownTerms(ctx context.Context, db Querier, d driver.Dialect, def *driver.TextIndex, keys []string) (map[string]bool, error) {
	ph := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, k := range keys {
		ph[i] = d.Param(i + 1)
		args[i] = k
	}
	rows, err := db.QueryContext(ctx, "SELECT term FROM "+def.LexiconTable()+" WHERE term IN ("+strings.Join(ph, ", ")+")", args...)
	if err != nil {
		return nil, fmt.Errorf("fts: reading the lexicon: %w", err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		known[t] = true
	}
	return known, rows.Err()
}

func bestCandidate(ctx context.Context, db Querier, query string, args []any, key string) (string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("fts: reading suggestions: %w", err)
	}
	defer rows.Close()
	type candidate struct {
		word string
		sim  float64
		ndoc int
	}
	var best *candidate
	for rows.Next() {
		var c candidate
		var term string
		if err := rows.Scan(&c.word, &term, &c.ndoc); err != nil {
			return "", err
		}
		if c.sim = similarity(key, term); c.sim < SuggestThreshold || term == key {
			continue
		}
		if best == nil || cmp.Or(cmp.Compare(c.sim, best.sim), cmp.Compare(c.ndoc, best.ndoc), strings.Compare(best.word, c.word)) > 0 {
			best = &c
		}
	}
	if err := rows.Err(); err != nil || best == nil {
		return "", err
	}
	return best.word, nil
}

// similarity is pg_trgm's: the trigrams of each word padded with two spaces
// before and one after, shared over all.
func similarity(a, b string) float64 {
	ta, tb := trigrams(a), trigrams(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	shared := 0
	for t := range ta {
		if tb[t] {
			shared++
		}
	}
	return float64(shared) / float64(len(ta)+len(tb)-shared)
}

func trigrams(w string) map[string]bool {
	rs := []rune("  " + w + " ")
	out := make(map[string]bool, len(rs))
	for i := 0; i+3 <= len(rs); i++ {
		out[string(rs[i:i+3])] = true
	}
	return out
}

// replaceWords rewrites the words of term whose folded form is in fix,
// keeping everything else (operators, quotes, spacing) as typed.
func replaceWords(term string, fix map[string]string) string {
	var b strings.Builder
	rs := []rune(term)
	for i := 0; i < len(rs); {
		if !isWordRune(rs[i]) {
			b.WriteRune(rs[i])
			i++
			continue
		}
		end := i
		for end < len(rs) && isWordRune(rs[end]) {
			end++
		}
		w := string(rs[i:end])
		if r, ok := fix[driver.FoldAccents(strings.ToLower(w))]; ok {
			w = r
		}
		b.WriteString(w)
		i = end
	}
	return b.String()
}

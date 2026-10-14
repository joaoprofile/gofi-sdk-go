package postgres

import (
	"strconv"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

var _ driver.FullTextDialect = PostgresDialect{}

// textConfig is the text search configuration of idx: the language, or a
// copy of it whose words pass through unaccent first.
func textConfig(idx *driver.TextIndex) string {
	if idx.Unaccent {
		return "gofi_" + idx.Language + "_unaccent"
	}
	return idx.Language
}

// regconfig is the configuration as a constant: an index expression only
// matches a query whose configuration is the same literal, never a parameter.
func regconfig(idx *driver.TextIndex) string {
	return "'" + textConfig(idx) + "'::regconfig"
}

func qualify(qualifier, column string) string {
	if qualifier == "" {
		return column
	}
	return qualifier + "." + column
}

// vectorExpr is the document of idx; the index and every query render it
// from the same definition, so the planner matches them.
func vectorExpr(idx *driver.TextIndex, qualifier string) string {
	var b strings.Builder
	for i, c := range idx.Columns {
		if i > 0 {
			b.WriteString(" || ")
		}
		v := "to_tsvector(" + regconfig(idx) + ", coalesce(" + qualify(qualifier, c.Name) + ", ''))"
		if c.Weight != 0 {
			v = "setweight(" + v + ", '" + string(c.Weight) + "')"
		}
		b.WriteString(v)
	}
	return b.String()
}

// document is what the index covers: the stored column or the expression.
func document(idx *driver.TextIndex, qualifier string) string {
	if idx.Strategy == driver.StoredVector {
		return qualify(qualifier, idx.VectorColumn)
	}
	return "(" + vectorExpr(idx, qualifier) + ")"
}

func tsquery(idx *driver.TextIndex, param string) string {
	return "to_tsquery(" + regconfig(idx) + ", " + param + ")"
}

// TextSearchValue renders q in to_tsquery syntax. Words hold only letters,
// digits and marks, so quoting them as 'word' is enough; stopwords are left
// to to_tsquery, which drops them.
func (PostgresDialect) TextSearchValue(_ *driver.TextIndex, q driver.TextQuery) (string, bool) {
	groups := make([]string, 0, len(q))
	for _, g := range q {
		terms := make([]string, 0, len(g))
		for _, t := range g {
			if len(t.Words) == 0 {
				continue
			}
			terms = append(terms, tsTerm(t))
		}
		if len(terms) > 0 {
			groups = append(groups, strings.Join(terms, " & "))
		}
	}
	switch len(groups) {
	case 0:
		return "", false
	case 1:
		return groups[0], true
	}
	return "( " + strings.Join(groups, " ) | ( ") + " )", true
}

// tsTerm renders a word, a word:* prefix, a 'phrase' <-> 'of words', a
// proximity (either order, 1 to Near positions apart) or the alternatives of
// a word and its synonyms.
func tsTerm(t driver.TextTerm) string {
	var s string
	grouped := false
	switch {
	case t.Near > 0 && len(t.Words) == 2:
		a, b := "'"+t.Words[0]+"'", "'"+t.Words[1]+"'"
		alts := make([]string, 0, 2*t.Near)
		for k := 1; k <= t.Near; k++ {
			d := " <" + strconv.Itoa(k) + "> "
			alts = append(alts, a+d+b, b+d+a)
		}
		s, grouped = "( "+strings.Join(alts, " | ")+" )", true
	case len(t.Synonyms) > 0 && len(t.Words) == 1:
		alts := []string{tsPhrase(t.Words)}
		for _, syn := range t.Synonyms {
			alts = append(alts, tsPhrase(syn))
		}
		s, grouped = "( "+strings.Join(alts, " | ")+" )", true
	default:
		s = tsPhrase(t.Words)
		if t.Prefix && len(t.Words) == 1 {
			s += ":*"
		}
	}
	if !t.Negate {
		return s
	}
	if len(t.Words) > 1 && !grouped {
		return "!(" + s + ")"
	}
	return "!" + s
}

func tsPhrase(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = "'" + w + "'"
	}
	return strings.Join(quoted, " <-> ")
}

func (d PostgresDialect) TextMatch(idx *driver.TextIndex, qualifier string, q driver.TextQuery, bind func(any) string) (string, bool) {
	v, ok := d.TextSearchValue(idx, q)
	if !ok {
		return "", false
	}
	return document(idx, qualifier) + " @@ " + tsquery(idx, bind(v)), true
}

// TextRank uses cover density, which rewards matched words close together;
// the column weights apply through the setweight labels of the document.
func (d PostgresDialect) TextRank(idx *driver.TextIndex, qualifier string, q driver.TextQuery, bind func(any) string) (string, bool) {
	v, ok := d.TextSearchValue(idx, q)
	if !ok {
		return "", false
	}
	return "ts_rank_cd(" + document(idx, qualifier) + ", " + tsquery(idx, bind(v)) + ")", true
}

// headlineOptions mark every matched word of the whole text with the
// private-use markers; the caller cuts excerpts.
const headlineOptions = `'StartSel="` + driver.HighlightStart + `", StopSel="` + driver.HighlightEnd + `", HighlightAll=true'`

// TextHighlight uses ts_headline with the index configuration, so it marks
// what the search matches: stems ("cafés" for "cafe") and folded accents.
// PostgreSQL evaluates it after ORDER BY … LIMIT: only the page pays for it.
func (d PostgresDialect) TextHighlight(idx *driver.TextIndex, qualifier, column string, q driver.TextQuery, bind func(any) string) (string, bool) {
	v, ok := d.TextSearchValue(idx, q)
	if !ok {
		return "", false
	}
	return "ts_headline(" + regconfig(idx) + ", coalesce(" + qualify(qualifier, column) + ", ''), " +
		tsquery(idx, bind(v)) + ", " + headlineOptions + ")", true
}

// indexName qualifies the index with the table's schema, where PostgreSQL creates it.
func indexName(idx *driver.TextIndex) string {
	if i := strings.LastIndexByte(idx.Table, '.'); i >= 0 {
		return idx.Table[:i+1] + idx.Name
	}
	return idx.Name
}

// TextIndexDDL creates, for Unaccent, the extension and a configuration
// copied from the language whose words pass through unaccent before the
// stemmer; the configuration is shared and kept on down. A Concurrently
// index is built in an isolated step: CREATE INDEX CONCURRENTLY cannot run
// inside a transaction block, so its comment follows in a step of its own.
func (PostgresDialect) TextIndexDDL(idx *driver.TextIndex, comment string) ([]driver.TextStep, error) {
	var steps []driver.TextStep
	if idx.Unaccent {
		cfg := textConfig(idx)
		stem := idx.Language + "_stem"
		if idx.Language == "simple" {
			stem = "simple"
		}
		steps = append(steps, driver.TextStep{Up: []string{
			"CREATE EXTENSION IF NOT EXISTS unaccent",
			"DO $$\nBEGIN\n" +
				"  IF NOT EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = '" + cfg + "') THEN\n" +
				"    CREATE TEXT SEARCH CONFIGURATION " + cfg + " (COPY = " + idx.Language + ");\n" +
				"    ALTER TEXT SEARCH CONFIGURATION " + cfg + " ALTER MAPPING FOR hword, hword_part, word WITH unaccent, " + stem + ";\n" +
				"  END IF;\n" +
				"END $$",
		}})
	}
	var commentStmt []string
	if comment != "" {
		commentStmt = []string{"COMMENT ON INDEX " + indexName(idx) + " IS '" + strings.ReplaceAll(comment, "'", "''") + "'"}
	}
	switch {
	case idx.Strategy == driver.StoredVector:
		steps = append(steps, driver.TextStep{
			Up: append([]string{
				"ALTER TABLE " + idx.Table + " ADD COLUMN " + idx.VectorColumn + " tsvector GENERATED ALWAYS AS (" + vectorExpr(idx, "") + ") STORED",
				"CREATE INDEX " + idx.Name + " ON " + idx.Table + " USING GIN (" + idx.VectorColumn + ")",
			}, commentStmt...),
			Down: []string{
				"DROP INDEX IF EXISTS " + indexName(idx),
				"ALTER TABLE " + idx.Table + " DROP COLUMN IF EXISTS " + idx.VectorColumn,
			},
		})
	case idx.Concurrently:
		steps = append(steps, driver.TextStep{
			Up:       []string{"CREATE INDEX CONCURRENTLY " + idx.Name + " ON " + idx.Table + " USING GIN ((" + vectorExpr(idx, "") + "))"},
			Down:     []string{"DROP INDEX CONCURRENTLY IF EXISTS " + indexName(idx)},
			Isolated: true,
		})
		if commentStmt != nil {
			steps = append(steps, driver.TextStep{Up: commentStmt})
		}
	default:
		steps = append(steps, driver.TextStep{
			Up:   append([]string{"CREATE INDEX " + idx.Name + " ON " + idx.Table + " USING GIN ((" + vectorExpr(idx, "") + "))"}, commentStmt...),
			Down: []string{"DROP INDEX IF EXISTS " + indexName(idx)},
		})
	}
	if idx.Suggest {
		lex := idx.LexiconTable()
		steps = append(steps, driver.TextStep{
			Up: []string{
				"CREATE EXTENSION IF NOT EXISTS pg_trgm",
				"CREATE TABLE " + lex + " (term text PRIMARY KEY, word text NOT NULL, ndoc integer NOT NULL)",
				"CREATE INDEX " + idx.Name + "_lexg ON " + lex + " USING GIN (term gin_trgm_ops)",
			},
			Down: []string{"DROP TABLE IF EXISTS " + lex},
		})
	}
	return steps, nil
}

// TextCandidates uses pg_trgm's % operator, served by the GIN trigram index
// (pg_trgm.similarity_threshold, 0.3 by default).
func (PostgresDialect) TextCandidates(idx *driver.TextIndex, key string, bind func(any) string) string {
	p := bind(key)
	return "SELECT word, term, ndoc FROM " + idx.LexiconTable() + " WHERE term % " + p +
		" ORDER BY similarity(term, " + p + ") DESC, ndoc DESC LIMIT 50"
}

// TextIndexLookup finds a valid GIN index of that name on the table,
// resolved through the search_path like the queries are. A CONCURRENTLY
// build that failed leaves an invalid index, which reads as missing.
func (PostgresDialect) TextIndexLookup(idx *driver.TextIndex) (string, []any) {
	return `SELECT COALESCE(obj_description(i.indexrelid, 'pg_class'), '')
FROM pg_index i
JOIN pg_class ic ON ic.oid = i.indexrelid
JOIN pg_am am ON am.oid = ic.relam
WHERE i.indrelid = to_regclass($1) AND ic.relname = $2 AND am.amname = 'gin' AND i.indisvalid`, []any{idx.Table, idx.Name}
}

// TextChecks requires the lexicon of Suggest; the configuration and the
// extensions are dependencies of the index or lexicon, which cannot exist
// without them.
func (PostgresDialect) TextChecks(idx *driver.TextIndex) []driver.TextCheck {
	if !idx.Suggest {
		return nil
	}
	return []driver.TextCheck{{
		Query: "SELECT CASE WHEN to_regclass($1) IS NULL THEN 'missing' ELSE 'present' END",
		Args:  []any{idx.LexiconTable()},
		Want:  "present",
		What:  "lexicon " + idx.LexiconTable(),
		Index: true,
	}}
}

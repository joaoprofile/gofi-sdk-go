package mysql

import (
	"bufio"
	"embed"
	"errors"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

var _ driver.FullTextDialect = MySQLDialect{}

// ErrConcurrentIndex reports a full-text index asked to build without
// blocking writes, which InnoDB cannot do.
var ErrConcurrentIndex = errors.New("mysql: a FULLTEXT index cannot be added without blocking writes; schedule the migration or drop Concurrently")

// innodbStopwords is InnoDB's default stopword list
// (information_schema.INNODB_FT_DEFAULT_STOPWORD). The index never holds
// them, so a required one would match nothing: searches drop them.
var innodbStopwords = map[string]bool{
	"a": true, "about": true, "an": true, "are": true, "as": true, "at": true, "be": true,
	"by": true, "com": true, "de": true, "en": true, "for": true, "from": true, "how": true,
	"i": true, "in": true, "is": true, "it": true, "la": true, "of": true, "on": true,
	"or": true, "that": true, "the": true, "this": true, "to": true, "was": true,
	"what": true, "when": true, "where": true, "who": true, "will": true, "with": true,
	"und": true, "www": true,
}

// stopwordFiles are PostgreSQL's lists (see stopwords/README.md): searches
// drop the stopwords of the index language, as PostgreSQL does, on top of
// InnoDB's list. A per-index InnoDB stopword table is not used: a table
// rebuild (OPTIMIZE, ALTER … FORCE) silently reverts it to the default list.
//
//go:embed stopwords/*.stop
var stopwordFiles embed.FS

var languageStopwords = sync.OnceValue(func() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	entries, _ := stopwordFiles.ReadDir("stopwords")
	for _, e := range entries {
		f, err := stopwordFiles.Open("stopwords/" + e.Name())
		if err != nil {
			continue
		}
		words := map[string]bool{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if w := strings.TrimSpace(sc.Text()); w != "" {
				words[fold(strings.ToLower(w))] = true
			}
		}
		_ = f.Close()
		out[strings.TrimSuffix(e.Name(), ".stop")] = words
	}
	return out
})

// fold compares words as an _ai_ collation does, so "nao" is the stopword "não".
func fold(s string) string { return driver.FoldAccents(s) }

func stopword(idx *driver.TextIndex, w string) bool {
	return languageStopwords()[idx.Language][fold(w)]
}

// indexable reports whether InnoDB indexes w and the language keeps it:
// shorter words and stopwords would be required words never matched.
func indexable(idx *driver.TextIndex, w string) bool {
	return utf8.RuneCountInString(w) >= idx.MinTokenSize && !innodbStopwords[w] && !stopword(idx, w)
}

// TextSearchValue renders q in boolean mode syntax: +word, -word, word*,
// "phrase" and (group) (group) for alternatives. Words hold only letters,
// digits and marks, so no operator can come from the search string.
func (MySQLDialect) TextSearchValue(idx *driver.TextIndex, q driver.TextQuery) (string, bool) {
	groups := make([]string, 0, len(q))
	for _, g := range q {
		terms := make([]string, 0, len(g))
		positive := false
		for _, t := range g {
			s, ok := booleanTerm(idx, t)
			if !ok {
				continue
			}
			op := "+"
			if t.Negate {
				op = "-"
			}
			terms = append(terms, op+s)
			positive = positive || !t.Negate
		}
		if positive {
			groups = append(groups, strings.Join(terms, " "))
		}
	}
	switch len(groups) {
	case 0:
		return "", false
	case 1:
		return groups[0], true
	}
	return "(" + strings.Join(groups, ") (") + ")", true
}

// booleanTerm renders t without its operator, or false when InnoDB would not
// match it. A phrase keeps its stopwords (InnoDB matches them in phrases)
// unless it has no other word; a prefix is kept, since it matches longer
// indexed words, unless it is a stopword of the language (PostgreSQL drops
// it). Synonyms become (word alternative "alternative phrase"): inside the
// parentheses, without operators, any one matches.
func booleanTerm(idx *driver.TextIndex, t driver.TextTerm) (string, bool) {
	switch {
	case len(t.Words) == 0:
		return "", false
	case t.Near > 0 && len(t.Words) == 2:
		// @N spans N words: N-1 positions apart, in either order.
		if indexable(idx, t.Words[0]) && indexable(idx, t.Words[1]) {
			return `"` + t.Words[0] + " " + t.Words[1] + `"@` + strconv.Itoa(t.Near+1), true
		}
		return phrase(idx, t.Words)
	case len(t.Words) > 1:
		return phrase(idx, t.Words)
	case t.Prefix:
		return t.Words[0] + "*", !stopword(idx, t.Words[0])
	}
	var alts []string
	for _, words := range append([][]string{t.Words}, t.Synonyms...) {
		if s, ok := phrase(idx, words); ok {
			alts = append(alts, s)
		}
	}
	switch len(alts) {
	case 0:
		return "", false
	case 1:
		return alts[0], true
	}
	return "(" + strings.Join(alts, " ") + ")", true
}

// phrase renders a word, or words as a "phrase", when InnoDB indexes one of them.
func phrase(idx *driver.TextIndex, words []string) (string, bool) {
	if len(words) == 1 {
		return words[0], indexable(idx, words[0])
	}
	for _, w := range words {
		if indexable(idx, w) {
			return `"` + strings.Join(words, " ") + `"`, true
		}
	}
	return "", false
}

// rankValue lists the positive terms of q without operators: a weight group
// holds only some columns, and a required term found in another group would
// zero its relevance.
func rankValue(idx *driver.TextIndex, q driver.TextQuery) string {
	var terms []string
	for _, g := range q {
		for _, t := range g {
			if s, ok := booleanTerm(idx, t); ok && !t.Negate {
				terms = append(terms, s)
			}
		}
	}
	return strings.Join(terms, " ")
}

// match lists exactly the columns of a FULLTEXT index: MySQL only searches
// a column list some index covers in the same set.
func match(cols []driver.TextColumn, qualifier, param string) string {
	var b strings.Builder
	b.WriteString("MATCH(")
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		if qualifier != "" {
			b.WriteString(qualifier + ".")
		}
		b.WriteString(c.Name)
	}
	b.WriteString(") AGAINST (" + param + " IN BOOLEAN MODE)")
	return b.String()
}

func (d MySQLDialect) TextMatch(idx *driver.TextIndex, qualifier string, q driver.TextQuery, bind func(any) string) (string, bool) {
	v, ok := d.TextSearchValue(idx, q)
	if !ok {
		return "", false
	}
	return match(idx.Columns, qualifier, bind(v)), true
}

// TextRank is the relevance MATCH returns. With column weights, it sums the
// relevance in each weight group's own index times the weight's factor.
func (d MySQLDialect) TextRank(idx *driver.TextIndex, qualifier string, q driver.TextQuery, bind func(any) string) (string, bool) {
	v, ok := d.TextSearchValue(idx, q)
	if !ok {
		return "", false
	}
	groups := idx.WeightGroups()
	if groups == nil {
		return match(idx.Columns, qualifier, bind(v)), true
	}
	rv := rankValue(idx, q)
	parts := make([]string, len(groups))
	for i, cols := range groups {
		parts[i] = match(cols, qualifier, bind(rv)) + " * " + cols[0].Weight.Factor()
	}
	return "(" + strings.Join(parts, " + ") + ")", true
}

// TextHighlight wraps the matched words with REGEXP_REPLACE: MySQL has no
// headline function. The pattern is built from the searched words (letters,
// digits and marks only), each letter widened to its accented forms, as the
// _ai_ collation compares them; whole words, a prefix marking the word it
// starts. There is no
// stemming, as in the search itself. Like the search, it skips negated
// terms and the words InnoDB never matches.
func (MySQLDialect) TextHighlight(idx *driver.TextIndex, qualifier, column string, q driver.TextQuery, bind func(any) string) (string, bool) {
	col := column
	if qualifier != "" {
		col = qualifier + "." + column
	}
	alts := highlightAlternatives(idx, q)
	if len(alts) == 0 {
		return "COALESCE(" + col + ", '')", true
	}
	pattern := `(?<![\p{L}\p{N}\p{M}])(?:` + strings.Join(alts, "|") + `)`
	return "REGEXP_REPLACE(COALESCE(" + col + ", ''), " + bind(pattern) + ", " +
		bind(driver.HighlightStart+"$0"+driver.HighlightEnd) + ", 1, 0, 'i')", true
}

func highlightAlternatives(idx *driver.TextIndex, q driver.TextQuery) []string {
	seen := map[string]bool{}
	var alts []string
	add := func(w string, prefix bool) {
		if (!prefix && !indexable(idx, w)) || (prefix && stopword(idx, w)) {
			return
		}
		// A prefix marks the whole word it starts, as PostgreSQL does.
		alt := accentClasses(w) + `[\p{L}\p{N}\p{M}]*`
		if !prefix {
			alt = accentClasses(w) + `(?![\p{L}\p{N}\p{M}])`
		}
		if !seen[alt] {
			seen[alt] = true
			alts = append(alts, alt)
		}
	}
	for _, g := range q {
		for _, t := range g {
			if t.Negate {
				continue
			}
			for _, words := range append([][]string{t.Words}, t.Synonyms...) {
				for _, w := range words {
					add(w, t.Prefix && len(t.Words) == 1)
				}
			}
		}
	}
	return alts
}

// accentClasses widens each letter with accented forms to a class: "cafe"
// becomes "caf[eàáâ…]". Words hold no regular expression metacharacters.
func accentClasses(w string) string {
	var b strings.Builder
	for _, r := range w {
		if forms := driver.AccentForms(r); forms != "" {
			b.WriteString("[" + forms + "]")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// groupIndexName names the FULLTEXT index of a weight group.
func groupIndexName(idx *driver.TextIndex, w driver.TextWeight) string {
	return idx.Name + "_w" + strings.ToLower(string(w.Effective()))
}

func columnList(cols []driver.TextColumn) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// TextIndexDDL adds the FULLTEXT index searched by TextMatch and, with
// column weights, one index per weight group for TextRank. InnoDB builds one
// FULLTEXT index per statement.
func (MySQLDialect) TextIndexDDL(idx *driver.TextIndex, comment string) ([]driver.TextStep, error) {
	if idx.Concurrently {
		return nil, ErrConcurrentIndex
	}
	main := "ALTER TABLE " + idx.Table + " ADD FULLTEXT INDEX " + idx.Name + " (" + columnList(idx.Columns) + ")"
	if comment != "" {
		main += " COMMENT '" + strings.ReplaceAll(comment, "'", "''") + "'"
	}
	step := driver.TextStep{Up: []string{main}}
	for _, cols := range idx.WeightGroups() {
		name := groupIndexName(idx, cols[0].Weight)
		step.Up = append(step.Up, "ALTER TABLE "+idx.Table+" ADD FULLTEXT INDEX "+name+" ("+columnList(cols)+")")
		step.Down = append(step.Down, "ALTER TABLE "+idx.Table+" DROP INDEX "+name)
	}
	step.Down = append(step.Down, "ALTER TABLE "+idx.Table+" DROP INDEX "+idx.Name)
	steps := []driver.TextStep{step}
	if idx.Suggest {
		lex := idx.LexiconTable()
		steps = append(steps, driver.TextStep{
			Up: []string{"CREATE TABLE " + lex + " (term VARCHAR(64) PRIMARY KEY, word VARCHAR(64) NOT NULL, ndoc INT NOT NULL, " +
				"FULLTEXT INDEX " + idx.Name + "_lexg (term) WITH PARSER ngram) DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci"},
			Down: []string{"DROP TABLE IF EXISTS " + lex},
		})
	}
	return steps, nil
}

// TextCandidates uses the lexicon's ngram FULLTEXT index: terms sharing the
// most n-grams with key come first.
func (MySQLDialect) TextCandidates(idx *driver.TextIndex, key string, bind func(any) string) string {
	m := "MATCH(term) AGAINST (" + bind(key) + " IN NATURAL LANGUAGE MODE)"
	return "SELECT word, term, ndoc FROM " + idx.LexiconTable() + " WHERE " + m +
		" ORDER BY MATCH(term) AGAINST (" + bind(key) + " IN NATURAL LANGUAGE MODE) DESC, ndoc DESC LIMIT 50"
}

// schemaTable splits schema.table; the schema defaults to DATABASE().
func schemaTable(idx *driver.TextIndex) (schemaExpr string, args []any) {
	if schema, table, ok := strings.Cut(idx.Table, "."); ok {
		return "?", []any{schema, table}
	}
	return "DATABASE()", []any{idx.Table}
}

// TextIndexLookup finds a FULLTEXT index of that name on the table, in the
// table's schema or the current database.
func (MySQLDialect) TextIndexLookup(idx *driver.TextIndex) (string, []any) {
	schema, args := schemaTable(idx)
	return `SELECT INDEX_COMMENT FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ` + schema + ` AND TABLE_NAME = ? AND INDEX_NAME = ? AND INDEX_TYPE = 'FULLTEXT'
LIMIT 1`, append(args, idx.Name)
}

// TextChecks requires the token size searches assume when dropping words,
// the weight group indexes TextRank reads and, with Unaccent, accent-
// insensitive collations: MySQL folds accents only through the collation.
func (MySQLDialect) TextChecks(idx *driver.TextIndex) []driver.TextCheck {
	checks := []driver.TextCheck{{
		Query: "SELECT CAST(@@innodb_ft_min_token_size AS CHAR)",
		Want:  strconv.Itoa(idx.MinTokenSize),
		What:  "innodb_ft_min_token_size",
	}}
	schema, args := schemaTable(idx)
	for _, cols := range idx.WeightGroups() {
		name := groupIndexName(idx, cols[0].Weight)
		checks = append(checks, driver.TextCheck{
			Query: `SELECT CAST(COUNT(*) AS CHAR) FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ` + schema + ` AND TABLE_NAME = ? AND INDEX_NAME = ? AND INDEX_TYPE = 'FULLTEXT'`,
			Args:  append(append([]any{}, args...), name),
			Want:  strconv.Itoa(len(cols)),
			What:  "columns of weight index " + name,
			Index: true,
		})
	}
	if idx.Suggest {
		lexSchema, lexArgs := schemaTable(&driver.TextIndex{Table: idx.LexiconTable()})
		checks = append(checks, driver.TextCheck{
			Query: "SELECT CAST(COUNT(*) AS CHAR) FROM information_schema.TABLES WHERE TABLE_SCHEMA = " + lexSchema + " AND TABLE_NAME = ?",
			Args:  lexArgs,
			Want:  "1",
			What:  "lexicon " + idx.LexiconTable(),
			Index: true,
		})
	}
	if idx.Unaccent {
		for _, c := range idx.Columns {
			checks = append(checks, driver.TextCheck{
				Query: `SELECT CASE WHEN COLLATION_NAME LIKE '%\_as\_%' OR COLLATION_NAME LIKE '%\_bin' OR COLLATION_NAME LIKE '%\_cs'
	THEN COLLATION_NAME ELSE 'accent-insensitive' END
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = ` + schema + ` AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
				Args: append(append([]any{}, args...), c.Name),
				Want: "accent-insensitive",
				What: "collation of " + c.Name,
			})
		}
	}
	return checks
}

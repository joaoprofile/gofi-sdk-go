package mysql

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

var _ driver.FullTextDialect = MySQLDialect{}

// stopwords is InnoDB's default stopword list
// (information_schema.INNODB_FT_DEFAULT_STOPWORD). A required stopword is
// never indexed, so "+cafe +the" would match nothing: searches drop them.
var stopwords = map[string]bool{
	"a": true, "about": true, "an": true, "are": true, "as": true, "at": true, "be": true,
	"by": true, "com": true, "de": true, "en": true, "for": true, "from": true, "how": true,
	"i": true, "in": true, "is": true, "it": true, "la": true, "of": true, "on": true,
	"or": true, "that": true, "the": true, "this": true, "to": true, "was": true,
	"what": true, "when": true, "where": true, "who": true, "will": true, "with": true,
	"und": true, "www": true,
}

// indexable reports whether InnoDB indexes w: shorter words and stopwords are
// skipped, and requiring one would match nothing.
func indexable(idx *driver.TextIndex, w string) bool {
	return utf8.RuneCountInString(w) >= idx.MinTokenSize && !stopwords[w]
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
			terms = append(terms, s)
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

// booleanTerm drops a word InnoDB does not index. A phrase keeps its
// stopwords (InnoDB matches them in phrases) unless it has no other word;
// a prefix is kept, since it matches longer indexed words.
func booleanTerm(idx *driver.TextIndex, t driver.TextTerm) (string, bool) {
	op := "+"
	if t.Negate {
		op = "-"
	}
	if len(t.Words) > 1 {
		for _, w := range t.Words {
			if indexable(idx, w) {
				return op + `"` + strings.Join(t.Words, " ") + `"`, true
			}
		}
		return "", false
	}
	if len(t.Words) == 0 {
		return "", false
	}
	w := t.Words[0]
	if t.Prefix {
		return op + w + "*", true
	}
	if !indexable(idx, w) {
		return "", false
	}
	return op + w, true
}

// match lists exactly the index columns: MySQL only searches a column list
// some FULLTEXT index covers in the same set.
func match(idx *driver.TextIndex, qualifier, param string) string {
	var b strings.Builder
	b.WriteString("MATCH(")
	for i, c := range idx.Columns {
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

func (MySQLDialect) TextMatch(idx *driver.TextIndex, qualifier, param string) string {
	return match(idx, qualifier, param)
}

// TextRank is the relevance MATCH returns; column weights do not apply.
func (MySQLDialect) TextRank(idx *driver.TextIndex, qualifier, param string) string {
	return match(idx, qualifier, param)
}

func (MySQLDialect) TextIndexDDL(idx *driver.TextIndex, comment string) (up, down []string) {
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = c.Name
	}
	stmt := "ALTER TABLE " + idx.Table + " ADD FULLTEXT INDEX " + idx.Name + " (" + strings.Join(cols, ", ") + ")"
	if comment != "" {
		stmt += " COMMENT '" + strings.ReplaceAll(comment, "'", "''") + "'"
	}
	return []string{stmt}, []string{"ALTER TABLE " + idx.Table + " DROP INDEX " + idx.Name}
}

// TextIndexLookup finds a FULLTEXT index of that name on the table, in the
// table's schema or the current database.
func (MySQLDialect) TextIndexLookup(idx *driver.TextIndex) (string, []any) {
	schema, table, ok := strings.Cut(idx.Table, ".")
	const q = `SELECT INDEX_COMMENT FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = %s AND TABLE_NAME = ? AND INDEX_NAME = ? AND INDEX_TYPE = 'FULLTEXT'
LIMIT 1`
	if !ok {
		return strings.Replace(q, "%s", "DATABASE()", 1), []any{schema, idx.Name}
	}
	return strings.Replace(q, "%s", "?", 1), []any{schema, table, idx.Name}
}

// TextChecks requires the token size searches assume when dropping words.
func (MySQLDialect) TextChecks(idx *driver.TextIndex) []driver.TextCheck {
	return []driver.TextCheck{{
		Query: "SELECT CAST(@@innodb_ft_min_token_size AS CHAR)",
		Want:  strconv.Itoa(idx.MinTokenSize),
		What:  "innodb_ft_min_token_size",
	}}
}

package connection

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatSQL lays out a statement for reading: one clause per line, the
// SELECT list one column per line, AND/OR of WHERE and HAVING on their own
// lines, JOINs indented, and subqueries indented inside their parentheses.
// Function-call parentheses stay inline. It only changes whitespace outside
// literals and quoted identifiers, so the statement means the same; text
// that is not valid UTF-8 is returned as is.
func FormatSQL(query string) string {
	if !utf8.ValidString(query) {
		return query
	}
	f := &sqlFormatter{toks: tokenizeSQL(query)}
	f.all()
	return strings.TrimSpace(f.b.String())
}

// all writes every token; a closing parenthesis without its opening one
// (malformed SQL, worth seeing in a log) is written and the rest follows.
func (f *sqlFormatter) all() {
	for i := f.level(0, 0, false); i < len(f.toks); i = f.level(i+1, 0, false) {
		f.write(f.toks[i], false)
	}
}

type sqlTokKind uint8

const (
	tokWord sqlTokKind = iota
	tokString
	tokQuoted
	tokOp
	tokOpen
	tokClose
	tokComma
)

type sqlTok struct {
	s    string
	kind sqlTokKind
}

func tokenizeSQL(q string) []sqlTok {
	var out []sqlTok
	rs := []rune(q)
	isWord := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' || r == '.'
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '\'' || r == '"' || r == '`':
			j := i + 1
			for j < len(rs) {
				if rs[j] == r {
					if r == '\'' && j+1 < len(rs) && rs[j+1] == '\'' { // '' inside a literal
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(rs))
			kind := tokQuoted
			if r == '\'' {
				kind = tokString
			}
			out = append(out, sqlTok{string(rs[i:end]), kind})
			i = end
		case r == '(':
			out = append(out, sqlTok{"(", tokOpen})
			i++
		case r == ')':
			out = append(out, sqlTok{")", tokClose})
			i++
		case r == ',':
			out = append(out, sqlTok{",", tokComma})
			i++
		case isWord(r):
			j := i
			for j < len(rs) && isWord(rs[j]) {
				j++
			}
			out = append(out, sqlTok{string(rs[i:j]), tokWord})
			i = j
		default:
			j := i + 1
			for j < len(rs) && strings.ContainsRune("<>=!|@&:+-*/%~^?#", rs[j]) && strings.ContainsRune("<>=!|@&:+-*/%~^?#", r) {
				j++
			}
			out = append(out, sqlTok{string(rs[i:j]), tokOp})
			i = j
		}
	}
	return out
}

// Clause keywords, longest first: each starts a line.
var (
	clauseKeywords = [][]string{
		{"LEFT", "OUTER", "JOIN"}, {"LEFT", "JOIN", "LATERAL"}, {"INSERT", "INTO"}, {"DELETE", "FROM"},
		{"GROUP", "BY"}, {"ORDER", "BY"}, {"UNION", "ALL"}, {"ON", "CONFLICT"},
		{"LEFT", "JOIN"}, {"RIGHT", "JOIN"}, {"INNER", "JOIN"}, {"FULL", "JOIN"}, {"CROSS", "JOIN"},
		{"SELECT"}, {"FROM"}, {"WHERE"}, {"HAVING"}, {"LIMIT"}, {"UNION"}, {"RETURNING"},
		{"VALUES"}, {"SET"}, {"UPDATE"}, {"WITH"}, {"JOIN"},
	}
	// sqlKeywords are colored when printing to a terminal.
	sqlKeywords = map[string]bool{
		"AND": true, "OR": true, "NOT": true, "IN": true, "IS": true, "NULL": true, "TRUE": true, "FALSE": true,
		"AS": true, "ON": true, "BETWEEN": true, "LIKE": true, "ILIKE": true, "ESCAPE": true, "ANY": true, "ALL": true,
		"ASC": true, "DESC": true, "NULLS": true, "FIRST": true, "LAST": true, "DISTINCT": true, "CASE": true,
		"WHEN": true, "THEN": true, "ELSE": true, "END": true, "EXISTS": true, "COALESCE": true, "COUNT": true,
		"INTO": true, "BY": true, "LATERAL": true, "OUTER": true, "INNER": true, "LEFT": true, "RIGHT": true,
		"FULL": true, "CROSS": true, "DO": true, "NOTHING": true, "CONFLICT": true, "OFFSET": true,
	}
)

type sqlFormatter struct {
	toks        []sqlTok
	b           strings.Builder
	last        sqlTok
	lastKeyword bool // last was written as a keyword: "FROM (", not "count("
	lineIndent  int  // indent of the current line: where a subquery nests
	// paint, when set, colors each token as it is written.
	paint func(t sqlTok, keyword bool) string
}

const indentUnit = "  "

func (f *sqlFormatter) newline(indent int) {
	f.b.WriteString("\n" + strings.Repeat(indentUnit, indent))
	f.last = sqlTok{s: "\n", kind: tokOp}
	f.lineIndent = indent
}

// write adds t with the spacing SQL is usually read with.
func (f *sqlFormatter) write(t sqlTok, keyword bool) {
	prev := f.last
	space := f.b.Len() > 0 && prev.s != "\n" && prev.kind != tokOpen && t.kind != tokClose && t.kind != tokComma &&
		t.s != "::" && prev.s != "::" && !(t.kind == tokOpen && prev.kind == tokWord && !f.lastKeyword && !isUpperKeyword(prev.s))
	if space {
		f.b.WriteByte(' ')
	}
	s := t.s
	if f.paint != nil {
		s = f.paint(t, keyword)
	}
	f.b.WriteString(s)
	f.last = t
	f.lastKeyword = keyword && isUpperKeyword(strings.ToUpper(t.s)) || keyword && !sqlKeywords[strings.ToUpper(t.s)]
}

func isUpperKeyword(s string) bool {
	return sqlKeywords[s] && s != "COUNT" && s != "COALESCE" && s != "ANY" && s != "EXISTS"
}

// clauseAt reports the clause keyword starting at i and its length in tokens.
func (f *sqlFormatter) clauseAt(i int) ([]string, int) {
	for _, kw := range clauseKeywords {
		if i+len(kw) > len(f.toks) {
			continue
		}
		ok := true
		for k, w := range kw {
			if t := f.toks[i+k]; t.kind != tokWord || !strings.EqualFold(t.s, w) {
				ok = false
				break
			}
		}
		if ok {
			return kw, len(kw)
		}
	}
	return nil, 0
}

func (f *sqlFormatter) startsSubquery(i int) bool {
	return i < len(f.toks) && f.toks[i].kind == tokWord &&
		(strings.EqualFold(f.toks[i].s, "SELECT") || strings.EqualFold(f.toks[i].s, "WITH"))
}

// level writes tokens from i until the closing parenthesis of this level
// and returns its index. inline levels (function arguments) break no lines.
func (f *sqlFormatter) level(i, indent int, inline bool) int {
	clause := ""
	between := false
	for i < len(f.toks) {
		t := f.toks[i]
		switch {
		case t.kind == tokClose:
			return i
		case t.kind == tokOpen:
			if f.startsSubquery(i + 1) {
				outer := f.lineIndent
				f.write(t, false)
				f.newline(outer + 1)
				i = f.level(i+1, outer+1, false)
				f.newline(outer)
			} else {
				f.write(t, false)
				i = f.level(i+1, indent, true)
			}
			if i < len(f.toks) {
				f.write(f.toks[i], false)
			}
			i++
			continue
		case t.kind == tokComma:
			f.write(t, false)
			if !inline && clause == "SELECT" {
				f.newline(indent + 1)
			}
			i++
			continue
		}
		if !inline {
			if kw, n := f.clauseAt(i); kw != nil {
				name := strings.Join(kw, " ")
				switch {
				case kw[len(kw)-1] == "JOIN" || kw[len(kw)-1] == "LATERAL":
					f.newline(indent + 1)
				case f.b.Len() > 0 && f.last.s != "\n":
					f.newline(indent)
				}
				for k := range n {
					f.write(f.toks[i+k], true)
				}
				clause = strings.ToUpper(name)
				if clause == "SELECT" {
					f.newline(indent + 1)
				}
				i += n
				continue
			}
			upper := strings.ToUpper(t.s)
			if t.kind == tokWord && (upper == "AND" || upper == "OR") && (clause == "WHERE" || clause == "HAVING") {
				if upper == "AND" && between {
					between = false
				} else {
					f.newline(indent + 1)
				}
			}
			if upper == "BETWEEN" {
				between = true
			}
		}
		f.write(t, t.kind == tokWord && sqlKeywords[strings.ToUpper(t.s)])
		i++
	}
	return i
}

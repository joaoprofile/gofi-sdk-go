package fts

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Mode selects how a search string is read.
type Mode uint8

const (
	// Websearch (default): words must all match; "quoted text" is a phrase,
	// "two words"~N finds them at most N positions apart in either order
	// (N up to MaxNear), -word or -"phrase" excludes, and OR separates
	// alternatives. Words of a Spec.Synonyms group match each other.
	Websearch Mode = iota
	// AllTerms: every word must match; no operators.
	AllTerms
	// AnyTerm: at least one word must match; no operators.
	AnyTerm
	// Prefix: every word must match as a word prefix, for search-as-you-type.
	Prefix
	// Phrase: the words must appear together, in order.
	Phrase
)

// Bounds of a search string.
const (
	MaxTermLength = 256 // characters
	MaxTerms      = 16  // words and phrases
	MaxNear       = 10  // positions of a "two words"~N proximity search
)

// Search is a search string on an Index. Build it with Index.Search and pass
// it to criteria.Match, criteria.ByRelevance or Query.SelectRelevance.
type Search struct {
	index     *Index
	term      string
	mode      Mode
	qualifier string
}

// Search returns a Websearch over the index for term, which may come from a
// request: it is always bound as a parameter and reduced to plain words.
func (i *Index) Search(term string) Search {
	return Search{index: i, term: term}
}

// Mode returns s read in mode m.
func (s Search) Mode(m Mode) Search {
	s.mode = m
	return s
}

// On qualifies the indexed columns with an alias. criteria resolves it from
// the query's FROM and JOIN tables; set it when the table is joined twice or
// when building a bare clause (criteria.BuildClause). Trusted input only.
func (s Search) On(qualifier string) Search {
	s.qualifier = qualifier
	return s
}

// Index returns the searched index.
func (s Search) Index() *Index { return s.index }

// Qualifier returns the alias set by On.
func (s Search) Qualifier() string { return s.qualifier }

// Query parses the search string. It fails with ErrInvalidSearch above the
// bounds and with ErrEmptySearch when no word remains.
func (s Search) Query() (driver.TextQuery, error) {
	if s.index == nil {
		return nil, fmt.Errorf("%w: search without an index", ErrInvalidIndex)
	}
	if n := utf8.RuneCountInString(s.term); n > MaxTermLength {
		return nil, fmt.Errorf("%w: %d characters exceed %d", ErrInvalidSearch, n, MaxTermLength)
	}
	q := parse(s.term, s.mode)
	n := 0
	for _, g := range q {
		n += len(g)
	}
	if n > MaxTerms {
		return nil, fmt.Errorf("%w: %d terms exceed %d", ErrInvalidSearch, n, MaxTerms)
	}
	if len(q) == 0 {
		return nil, ErrEmptySearch
	}
	s.index.expand(q)
	return q, nil
}

// Value renders the search string bound to the match and rank expressions of d.
func (s Search) Value(d driver.FilterDialect) (string, error) {
	fd, ok := d.(driver.FullTextDialect)
	if !ok {
		return "", fmt.Errorf("%w: %T", ErrUnsupported, d)
	}
	q, err := s.Query()
	if err != nil {
		return "", err
	}
	v, ok := fd.TextSearchValue(&s.index.def, q)
	if !ok {
		return "", ErrEmptySearch
	}
	return v, nil
}

func parse(term string, mode Mode) driver.TextQuery {
	switch mode {
	case AllTerms, Prefix:
		words := splitWords(term)
		g := make(driver.TextGroup, len(words))
		for i, w := range words {
			g[i] = driver.TextTerm{Words: []string{w}, Prefix: mode == Prefix}
		}
		return keepPositive(driver.TextQuery{g})
	case AnyTerm:
		var q driver.TextQuery
		for _, w := range splitWords(term) {
			q = append(q, driver.TextGroup{{Words: []string{w}}})
		}
		return q
	case Phrase:
		words := splitWords(term)
		if len(words) == 0 {
			return nil
		}
		return driver.TextQuery{{{Words: words}}}
	default:
		return parseWebsearch(term)
	}
}

// parseWebsearch reads words, "phrases", "two words"~N, -exclusions and OR.
// A '-' only negates at the start of a token: in "e-mail" it separates two
// words.
func parseWebsearch(term string) driver.TextQuery {
	var q driver.TextQuery
	var g driver.TextGroup
	rs := []rune(term)
	for i := 0; i < len(rs); {
		r := rs[i]
		negate := false
		if r == '-' && (i == 0 || unicode.IsSpace(rs[i-1])) && i+1 < len(rs) && (rs[i+1] == '"' || isWordRune(rs[i+1])) {
			negate = true
			i++
			r = rs[i]
		}
		switch {
		case r == '"':
			end := i + 1
			for end < len(rs) && rs[end] != '"' {
				end++
			}
			words := splitWords(string(rs[i+1 : end]))
			i = end + 1
			near := 0
			if i < len(rs) && rs[i] == '~' {
				j := i + 1
				for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' && j-i <= 3 {
					near = near*10 + int(rs[j]-'0')
					j++
				}
				i = j
			}
			if len(words) > 0 {
				t := driver.TextTerm{Words: words, Negate: negate}
				if len(words) == 2 && near > 0 {
					t.Near = min(near, MaxNear)
				}
				g = append(g, t)
			}
		case isWordRune(r):
			end := i
			for end < len(rs) && isWordRune(rs[end]) {
				end++
			}
			w := strings.ToLower(string(rs[i:end]))
			if w == "or" && !negate {
				if len(g) > 0 {
					q = append(q, g)
					g = nil
				}
			} else {
				g = append(g, driver.TextTerm{Words: []string{w}, Negate: negate})
			}
			i = end
		default:
			i++
		}
	}
	if len(g) > 0 {
		q = append(q, g)
	}
	return keepPositive(q)
}

// keepPositive drops groups without a positive term: an exclusion alone
// would match almost every row and cannot use the index.
func keepPositive(q driver.TextQuery) driver.TextQuery {
	out := q[:0]
	for _, g := range q {
		for _, t := range g {
			if !t.Negate {
				out = append(out, g)
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// splitWords returns the lower-cased runs of letters, digits and marks.
func splitWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !isWordRune(r) })
}

package driver

// TextWeight ranks the matches of one indexed column; A ranks highest. The
// zero value leaves the column unweighted, which ranks as D.
type TextWeight byte

const (
	WeightA TextWeight = 'A'
	WeightB TextWeight = 'B'
	WeightC TextWeight = 'C'
	WeightD TextWeight = 'D'
)

// Effective is w, or D for an unweighted column.
func (w TextWeight) Effective() TextWeight {
	if w == 0 {
		return WeightD
	}
	return w
}

// Factor is the share of a match in a column of weight w, the defaults of
// PostgreSQL's ts_rank ({0.1, 0.2, 0.4, 1.0} for D, C, B, A); dialects that
// emulate weights use the same factors so both rank alike.
func (w TextWeight) Factor() string {
	switch w.Effective() {
	case WeightA:
		return "1.0"
	case WeightB:
		return "0.4"
	case WeightC:
		return "0.2"
	default:
		return "0.1"
	}
}

// TextColumn is one column of a full-text index.
type TextColumn struct {
	Name   string
	Weight TextWeight
}

// TextStrategy selects how PostgreSQL materializes the searched document.
type TextStrategy uint8

const (
	// StoredVector adds a generated tsvector column and indexes it: ranking
	// reads the stored vector instead of parsing the text again per row.
	StoredVector TextStrategy = iota
	// ExpressionIndex indexes the tsvector expression itself: no new column
	// and no table rewrite, but ranking recomputes the vector per row.
	ExpressionIndex
)

// TextIndex is the validated definition of a full-text index. The same
// definition renders the DDL and every query on it, so a query always reads
// the expression or column list its index was built on. Build it with
// fts.NewIndex, never by hand.
type TextIndex struct {
	Name         string
	Table        string // table or schema.table
	Columns      []TextColumn
	Language     string // text search language, e.g. "portuguese"
	Unaccent     bool   // fold accents (PostgreSQL unaccent, MySQL _ai_ collation)
	Strategy     TextStrategy
	VectorColumn string // PostgreSQL StoredVector: the generated column
	MinTokenSize int    // MySQL: innodb_ft_min_token_size the index was built with
	Concurrently bool   // build without blocking writes (PostgreSQL)
}

// WeightGroups returns the columns by effective weight, A first, or nil when
// every column has the same weight.
func (idx *TextIndex) WeightGroups() [][]TextColumn {
	byWeight := map[TextWeight][]TextColumn{}
	for _, c := range idx.Columns {
		w := c.Weight.Effective()
		byWeight[w] = append(byWeight[w], c)
	}
	if len(byWeight) < 2 {
		return nil
	}
	var groups [][]TextColumn
	for _, w := range []TextWeight{WeightA, WeightB, WeightC, WeightD} {
		if cols := byWeight[w]; len(cols) > 0 {
			groups = append(groups, cols)
		}
	}
	return groups
}

// TextTerm is one word or, with several words, one phrase of a search.
type TextTerm struct {
	Words  []string // lower-case letters and digits only
	Prefix bool     // matches words starting with the (single) word
	Negate bool     // excludes rows containing the term
	// Near, for two words, matches them at most Near positions apart, in
	// either order, instead of as an exact phrase.
	Near int
	// Synonyms are alternatives to the (single) word, each a word or a
	// phrase: the term matches when any of them does.
	Synonyms [][]string
}

// TextGroup is a conjunction: every positive term must match, no negated term may.
type TextGroup []TextTerm

// TextQuery is a parsed search in disjunctive form: a row matches when any
// group matches. Words carry no operator characters, so rendering them into
// a search string cannot change its structure.
type TextQuery []TextGroup

// TextCheck is a catalog query whose single string result must equal Want
// for the index to be usable, e.g. a server setting the index depends on.
type TextCheck struct {
	Query string
	Args  []any
	Want  string
	What  string // describes the requirement in errors
	// Index marks a part of the index itself (e.g. a MySQL weight group
	// index): a mismatch means the index is missing, not misconfigured.
	Index bool
}

// TextStep is a group of DDL statements that run together. An Isolated
// step holds statements that cannot run inside a transaction block, such as
// CREATE INDEX CONCURRENTLY: each gets a migration file of its own.
type TextStep struct {
	Up, Down []string
	Isolated bool
}

// FullTextDialect is implemented by dialects with full-text search.
type FullTextDialect interface {
	// TextSearchValue renders q as the search string of TextMatch; ok is
	// false when nothing searchable remains.
	TextSearchValue(idx *TextIndex, q TextQuery) (value string, ok bool)
	// TextMatch renders the condition matching q against idx, whose columns
	// are qualified by qualifier (an alias or table, may be empty). bind
	// binds a search string and returns its placeholder.
	TextMatch(idx *TextIndex, qualifier string, q TextQuery, bind func(any) string) (expr string, ok bool)
	// TextRank renders the relevance of q in idx; higher is better.
	TextRank(idx *TextIndex, qualifier string, q TextQuery, bind func(any) string) (expr string, ok bool)
	// TextIndexDDL renders the steps creating and dropping idx; comment is
	// stored on the index, where TextIndexLookup reads it back.
	TextIndexDDL(idx *TextIndex, comment string) ([]TextStep, error)
	// TextIndexLookup selects the comment of idx, or no row when idx does
	// not exist, usable, with the expected kind.
	TextIndexLookup(idx *TextIndex) (query string, args []any)
	// TextChecks lists the requirements of idx beyond the index itself.
	TextChecks(idx *TextIndex) []TextCheck
}

package driver

// TextWeight ranks the matches of one indexed column; A ranks highest. The
// zero value leaves the column unweighted. Only PostgreSQL applies weights.
type TextWeight byte

const (
	WeightA TextWeight = 'A'
	WeightB TextWeight = 'B'
	WeightC TextWeight = 'C'
	WeightD TextWeight = 'D'
)

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
	Unaccent     bool   // PostgreSQL: fold accents through the unaccent extension
	Strategy     TextStrategy
	VectorColumn string // PostgreSQL StoredVector: the generated column
	MinTokenSize int    // MySQL: innodb_ft_min_token_size the index was built with
}

// TextTerm is one word or, with several words, one phrase of a search.
type TextTerm struct {
	Words  []string // lower-case letters and digits only
	Prefix bool     // matches words starting with the (single) word
	Negate bool     // excludes rows containing the term
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
}

// FullTextDialect is implemented by dialects with full-text search.
type FullTextDialect interface {
	// TextSearchValue renders q as the search string bound to the match and
	// rank expressions; ok is false when nothing searchable remains.
	TextSearchValue(idx *TextIndex, q TextQuery) (value string, ok bool)
	// TextMatch renders the condition matching param against idx, whose
	// columns are qualified by qualifier (an alias or table, may be empty).
	TextMatch(idx *TextIndex, qualifier, param string) string
	// TextRank renders the relevance of param in idx; higher is better.
	TextRank(idx *TextIndex, qualifier, param string) string
	// TextIndexDDL renders the statements creating and dropping idx; comment
	// is stored on the index, where TextIndexLookup reads it back.
	TextIndexDDL(idx *TextIndex, comment string) (up, down []string)
	// TextIndexLookup selects the comment of idx, or no row when idx does
	// not exist with the expected kind.
	TextIndexLookup(idx *TextIndex) (query string, args []any)
	// TextChecks lists the server requirements of idx beyond the index itself.
	TextChecks(idx *TextIndex) []TextCheck
}

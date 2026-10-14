package fts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Weight ranks the matches of a column; A ranks highest, an unweighted
// column ranks as D. PostgreSQL labels the vector; MySQL adds one FULLTEXT
// index per weight group and sums their relevance with the same factors.
type Weight = driver.TextWeight

const (
	A = driver.WeightA
	B = driver.WeightB
	C = driver.WeightC
	D = driver.WeightD
)

// Strategy selects how PostgreSQL materializes the searched document.
type Strategy = driver.TextStrategy

const (
	// StoredVector (default) adds a generated tsvector column and indexes it.
	// Ranking is cheap; adding the column rewrites the table.
	StoredVector = driver.StoredVector
	// ExpressionIndex indexes the tsvector expression: no column and no
	// rewrite, but ranking parses the text again for every matched row.
	ExpressionIndex = driver.ExpressionIndex
)

// Language selects stopwords and, on PostgreSQL, stemming. MySQL has no
// stemmer: searches drop the language's stopwords (PostgreSQL's lists), so
// both ignore the same words, but "cafés" does not find "café" there.
type Language string

const (
	Simple     Language = "simple" // no stemming, no stopwords
	Portuguese Language = "portuguese"
	English    Language = "english"
	Spanish    Language = "spanish"
	French     Language = "french"
	German     Language = "german"
	Italian    Language = "italian"
	Dutch      Language = "dutch"
)

// languages are the PostgreSQL built-in configurations; each but simple has
// a <language>_stem snowball dictionary.
var languages = map[Language]bool{
	Simple: true, Portuguese: true, English: true, Spanish: true, French: true,
	German: true, Italian: true, Dutch: true, "arabic": true, "armenian": true,
	"basque": true, "catalan": true, "danish": true, "finnish": true, "greek": true,
	"hindi": true, "hungarian": true, "indonesian": true, "irish": true,
	"lithuanian": true, "nepali": true, "norwegian": true, "romanian": true,
	"russian": true, "serbian": true, "swedish": true, "tamil": true,
	"turkish": true, "yiddish": true,
}

// DefaultMinTokenSize is MySQL's default innodb_ft_min_token_size.
const DefaultMinTokenSize = 3

// Column is one indexed column.
type Column struct {
	Name   string
	Weight Weight
}

// Spec describes a full-text index.
type Spec struct {
	// Name of the index: lower-case letters, digits and '_', at most 59
	// characters. It must be unique in the schema.
	Name    string
	Columns []Column
	// Language defaults to Simple.
	Language Language
	// Unaccent folds accents: PostgreSQL through the unaccent extension,
	// which the migration creates; MySQL through the column collation, which
	// the check requires to be accent-insensitive (_ai_, or a legacy _ci).
	Unaccent bool
	// Strategy defaults to StoredVector.
	Strategy Strategy
	// VectorColumn names the StoredVector column; defaults to Name + "_tsv".
	VectorColumn string
	// MinTokenSize is the innodb_ft_min_token_size of the MySQL server,
	// DefaultMinTokenSize when zero. Shorter words are dropped from searches:
	// MySQL would otherwise require a word it never indexed and match nothing.
	MinTokenSize int
	// Synonyms lists groups of equivalent words or phrases, e.g.
	// {"notebook", "laptop"}: a searched word also matches the other members
	// of its group, on both databases. They apply to queries only, so
	// changing them needs no migration.
	Synonyms [][]string
	// Concurrently builds the index without blocking writes (PostgreSQL
	// CREATE INDEX CONCURRENTLY, in a migration file of its own). It needs
	// ExpressionIndex, since StoredVector rewrites the table anyway. MySQL
	// cannot: rendering its migration fails.
	Concurrently bool
}

// Index is a validated full-text index definition. Declare each one once
// with MustIndex, as a package-level variable (in the repository that
// searches it, for instance), and share it between the migration and the
// queries.
type Index struct {
	def      driver.TextIndex
	synonyms map[string][][]string // word → alternatives, from Spec.Synonyms
}

var (
	nameRe   = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,58}$`)
	columnRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	tableRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}(\.[A-Za-z_][A-Za-z0-9_]{0,62})?$`)
)

// NewIndex validates spec for table (name or schema.name, unquoted). It does
// not register the index (see MustIndex): use it for indexes built at run
// time and in tests.
func NewIndex(table string, spec Spec) (*Index, error) {
	if !tableRe.MatchString(table) {
		return nil, fmt.Errorf("%w: table %q must be name or schema.name", ErrInvalidIndex, table)
	}
	if !nameRe.MatchString(spec.Name) {
		return nil, fmt.Errorf("%w: name %q must be lower-case letters, digits and '_' (at most 59)", ErrInvalidIndex, spec.Name)
	}
	if len(spec.Columns) == 0 {
		return nil, fmt.Errorf("%w: %s has no columns", ErrInvalidIndex, spec.Name)
	}
	cols := make([]driver.TextColumn, len(spec.Columns))
	seen := make(map[string]bool, len(spec.Columns))
	for i, c := range spec.Columns {
		if !columnRe.MatchString(c.Name) {
			return nil, fmt.Errorf("%w: %s: column %q is not a plain name", ErrInvalidIndex, spec.Name, c.Name)
		}
		if seen[strings.ToLower(c.Name)] {
			return nil, fmt.Errorf("%w: %s: column %q repeated", ErrInvalidIndex, spec.Name, c.Name)
		}
		seen[strings.ToLower(c.Name)] = true
		switch c.Weight {
		case 0, A, B, C, D:
		default:
			return nil, fmt.Errorf("%w: %s: column %q has weight %q, want A-D", ErrInvalidIndex, spec.Name, c.Name, c.Weight)
		}
		cols[i] = driver.TextColumn{Name: c.Name, Weight: c.Weight}
	}
	lang := spec.Language
	if lang == "" {
		lang = Simple
	}
	if !languages[lang] {
		return nil, fmt.Errorf("%w: %s: unknown language %q", ErrInvalidIndex, spec.Name, lang)
	}
	if spec.Strategy != StoredVector && spec.Strategy != ExpressionIndex {
		return nil, fmt.Errorf("%w: %s: unknown strategy %d", ErrInvalidIndex, spec.Name, spec.Strategy)
	}
	vec := spec.VectorColumn
	if vec == "" {
		vec = spec.Name + "_tsv"
	}
	if !columnRe.MatchString(vec) || seen[strings.ToLower(vec)] {
		return nil, fmt.Errorf("%w: %s: invalid vector column %q", ErrInvalidIndex, spec.Name, vec)
	}
	if spec.Concurrently && spec.Strategy != ExpressionIndex {
		return nil, fmt.Errorf("%w: %s: Concurrently needs ExpressionIndex (StoredVector rewrites the table)", ErrInvalidIndex, spec.Name)
	}
	minToken := spec.MinTokenSize
	if minToken == 0 {
		minToken = DefaultMinTokenSize
	}
	if minToken < 1 || minToken > 16 {
		return nil, fmt.Errorf("%w: %s: MinTokenSize %d outside 1-16", ErrInvalidIndex, spec.Name, minToken)
	}
	synonyms, err := synonymMap(spec.Name, spec.Synonyms)
	if err != nil {
		return nil, err
	}
	return &Index{synonyms: synonyms, def: driver.TextIndex{
		Name:         spec.Name,
		Table:        table,
		Columns:      cols,
		Language:     string(lang),
		Unaccent:     spec.Unaccent,
		Strategy:     spec.Strategy,
		VectorColumn: vec,
		MinTokenSize: minToken,
		Concurrently: spec.Concurrently,
	}}, nil
}

// MustIndex declares an index: it validates spec like NewIndex and registers
// the index (see Registered), so the service checks it at startup. Call it
// at package level; it panics on an invalid spec, and on a table and name
// already declared with another definition.
func MustIndex(table string, spec Spec) *Index {
	idx, err := NewIndex(table, spec)
	if err == nil {
		err = register(idx)
	}
	if err != nil {
		panic(err)
	}
	return idx
}

// Name returns the index name.
func (i *Index) Name() string { return i.def.Name }

// Table returns the indexed table as given to NewIndex.
func (i *Index) Table() string { return i.def.Table }

// Definition returns a copy of the validated definition the dialects render.
func (i *Index) Definition() *driver.TextIndex {
	def := i.def
	def.Columns = append([]driver.TextColumn(nil), i.def.Columns...)
	return &def
}

// commentPrefix marks the comment sqln stores on the indexes it creates.
const commentPrefix = "gofi-fts:v1:"

// Comment returns the comment the migration stores on the index for d: a
// hash of its DDL, so Check detects an index built from another definition.
// Concurrently only changes how the index is built, not what it holds, so
// it is left out of the hash.
func (i *Index) Comment(d driver.Dialect) (string, error) {
	fd, err := fullText(d)
	if err != nil {
		return "", err
	}
	def := i.def
	def.Concurrently = false
	steps, err := fd.TextIndexDDL(&def, "")
	if err != nil {
		return "", err
	}
	var up []string
	for _, s := range steps {
		up = append(up, s.Up...)
	}
	sum := sha256.Sum256([]byte(strings.Join(up, ";\n")))
	return commentPrefix + hex.EncodeToString(sum[:8]), nil
}

func fullText(d driver.Dialect) (driver.FullTextDialect, error) {
	fd, ok := d.(driver.FullTextDialect)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrUnsupported, d)
	}
	return fd, nil
}

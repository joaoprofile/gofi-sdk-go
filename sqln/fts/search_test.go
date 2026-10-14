package fts_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/oracle"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

var products = fts.MustIndex("products", fts.Spec{
	Name:     "ftx_products_search",
	Columns:  []fts.Column{{Name: "name", Weight: fts.A}, {Name: "description", Weight: fts.B}},
	Language: fts.Portuguese,
	Unaccent: true,
})

func TestSearchValue(t *testing.T) {
	tests := []struct {
		name, term string
		mode       fts.Mode
		pg, my     string
	}{
		{"words", "Notebook Gamer", fts.Websearch, "'notebook' & 'gamer'", "+notebook +gamer"},
		{"accents kept for the database to fold", "Café Elétrico", fts.Websearch, "'café' & 'elétrico'", "+café +elétrico"},
		{"phrase and exclusion", `"máquina de café" -usado`, fts.Websearch,
			"'máquina' <-> 'de' <-> 'café' & !'usado'", `+"máquina de café" -usado`},
		{"negated phrase", `cafe -"de vidro"`, fts.Websearch, "'cafe' & !('de' <-> 'vidro')", `+cafe -"de vidro"`},
		{"or groups", "cafe expresso OR cha", fts.Websearch,
			"( 'cafe' & 'expresso' ) | ( 'cha' )", "(+cafe +expresso) (+cha)"},
		{"lower-case or", "cafe or cha", fts.Websearch, "( 'cafe' ) | ( 'cha' )", "(+cafe) (+cha)"},
		{"hyphen inside a word separates", "e-mail", fts.Websearch, "'e' & 'mail'", "+mail"},
		{"mysql drops short words and stopwords", "tv de 50 polegadas the", fts.Websearch,
			"'tv' & 'de' & '50' & 'polegadas' & 'the'", "+polegadas"},
		{"prefix", "note gam", fts.Prefix, "'note':* & 'gam':*", "+note* +gam*"},
		{"prefix keeps short words", "tv", fts.Prefix, "'tv':*", "+tv*"},
		{"all terms ignores operators", `-cafe "OR" cha`, fts.AllTerms, "'cafe' & 'or' & 'cha'", "+cafe +cha"},
		{"any term", "cafe cha", fts.AnyTerm, "( 'cafe' ) | ( 'cha' )", "(+cafe) (+cha)"},
		{"phrase mode", "maquina de cafe", fts.Phrase, "'maquina' <-> 'de' <-> 'cafe'", `+"maquina de cafe"`},
		{"operators are stripped", `cafe' & !x:* | (y) <-> z\ *+@~<>`, fts.Websearch,
			"'cafe' & 'x' & 'y' & 'z'", "+cafe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := products.Search(tt.term).Mode(tt.mode)
			pg, err := s.Value(postgres.PostgresDialect{})
			require.NoError(t, err)
			assert.Equal(t, tt.pg, pg)
			my, err := s.Value(mysql.MySQLDialect{})
			require.NoError(t, err)
			assert.Equal(t, tt.my, my)
		})
	}
}

func TestSearchEmptyAndBounds(t *testing.T) {
	for _, term := range []string{"", "   ", "!!! ---", "-cafe", `-"de vidro"`, "OR", `""`} {
		_, err := products.Search(term).Value(postgres.PostgresDialect{})
		assert.ErrorIs(t, err, fts.ErrEmptySearch, "%q", term)
	}
	// Valid for PostgreSQL, but MySQL never indexed any of the words.
	_, err := products.Search("tv de a").Value(mysql.MySQLDialect{})
	assert.ErrorIs(t, err, fts.ErrEmptySearch)
	_, err = products.Search("cafe OR tv").Value(mysql.MySQLDialect{})
	assert.NoError(t, err)

	_, err = products.Search(strings.Repeat("a", fts.MaxTermLength+1)).Value(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, fts.ErrInvalidSearch)
	_, err = products.Search(strings.Repeat("ab ", fts.MaxTerms+1)).Value(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, fts.ErrInvalidSearch)

	_, err = products.Search("cafe").Value(oracle.OracleDialect{})
	assert.ErrorIs(t, err, fts.ErrUnsupported)
	_, err = (*fts.Index)(nil).Search("cafe").Value(postgres.PostgresDialect{})
	assert.ErrorIs(t, err, fts.ErrInvalidIndex)
}

// Each rendered value must be the grammar the dialect renderer writes, with
// words of letters, digits and marks only: no input reaches the syntax.
var (
	word   = `'[\pL\pN\pM]+'`
	phr    = word + `(?: <-> ` + word + `)*`
	alt    = `(?:` + phr + `|` + word + ` <\d+> ` + word + `)`
	pgTerm = `!?(?:` + word + `(?::\*)?|\(?` + phr + `\)?|\( ` + alt + `(?: \| ` + alt + `)+ \))`
	pgAnd  = pgTerm + `(?: & ` + pgTerm + `)*`
	pgRe   = regexp.MustCompile(`^(?:` + pgAnd + `|\( ` + pgAnd + ` \)(?: \| \( ` + pgAnd + ` \))+)$`)
	myWord = `[\pL\pN\pM]+`
	myItem = `(?:` + myWord + `\*?|"` + myWord + `(?: ` + myWord + `)+"(?:@\d+)?)`
	myTerm = `[+-](?:` + myItem + `|\(` + myItem + `(?: ` + myItem + `)+\))`
	myAnd  = myTerm + `(?: ` + myTerm + `)*`
	myRe   = regexp.MustCompile(`^(?:` + myAnd + `|\(` + myAnd + `\)(?: \(` + myAnd + `\))+)$`)
)

var synonymSearch = fts.MustIndex("products", fts.Spec{
	Name:     "ftx_products_synonyms",
	Columns:  []fts.Column{{Name: "name"}},
	Language: fts.Portuguese,
	Synonyms: [][]string{{"notebook", "laptop", "computador portatil"}, {"celular", "smartphone"}, {"tv", "televisao", "televisor"}},
})

func FuzzSearchValue(f *testing.F) {
	for _, s := range []string{"cafe", `"a b" -c OR d`, `x' & !y:* | (z)`, "-\"\" OR OR -", "ação ñ 中文", "a\x00b", "''''"} {
		f.Add(s, uint8(0))
	}
	f.Add(`"cafe coado"~3 notebook -celular`, uint8(0))
	f.Fuzz(func(t *testing.T, term string, mode uint8) {
		idx := products
		if mode&8 != 0 {
			idx = synonymSearch
		}
		s := idx.Search(term).Mode(fts.Mode(mode % 5))
		if v, err := s.Value(postgres.PostgresDialect{}); err == nil && !pgRe.MatchString(v) {
			t.Fatalf("postgres value %q for %q escapes the grammar", v, term)
		} else if err != nil && !errors.Is(err, fts.ErrEmptySearch) && !errors.Is(err, fts.ErrInvalidSearch) {
			t.Fatalf("unexpected error %v", err)
		}
		if v, err := s.Value(mysql.MySQLDialect{}); err == nil && !myRe.MatchString(v) {
			t.Fatalf("mysql value %q for %q escapes the grammar", v, term)
		}
	})
}

func TestMySQLDropsLanguageStopwords(t *testing.T) {
	my := mysql.MySQLDialect{}
	english := mustNew(t, "products", fts.Spec{Name: "ftx_en", Columns: []fts.Column{{Name: "name"}}, Language: fts.English})
	simple := mustNew(t, "products", fts.Spec{Name: "ftx_simple", Columns: []fts.Column{{Name: "name"}}})

	// "para" and "não" are Portuguese stopwords, as PostgreSQL's list has them;
	// "nao" is folded like an _ai_ collation compares it.
	v, err := products.Search("cafe para viagem nao sem açúcar").Value(my)
	require.NoError(t, err)
	assert.Equal(t, "+cafe +viagem +açúcar", v)

	v, err = english.Search("cafe para viagem nao sem açúcar").Value(my)
	require.NoError(t, err)
	assert.Equal(t, "+cafe +para +viagem +nao +sem +açúcar", v, "other languages keep them")

	v, err = english.Search("coffee with milk without sugar").Value(my)
	require.NoError(t, err)
	assert.Equal(t, "+coffee +milk +without +sugar", v, "exactly PostgreSQL's list: with is a stopword, without is not")

	v, err = simple.Search("para what about").Value(my)
	require.NoError(t, err)
	assert.Equal(t, "+para", v, "simple drops only InnoDB's own list")

	// A stopword prefix is dropped as PostgreSQL drops it; others are kept.
	v, err = products.Search("para cafe").Mode(fts.Prefix).Value(my)
	require.NoError(t, err)
	assert.Equal(t, "+cafe*", v)
}

func TestSynonyms(t *testing.T) {
	pg, my := postgres.PostgresDialect{}, mysql.MySQLDialect{}
	tests := []struct{ term, pg, my string }{
		{"notebook gamer", "( 'notebook' | 'laptop' | 'computador' <-> 'portatil' ) & 'gamer'",
			`+(notebook laptop "computador portatil") +gamer`},
		{"laptop", "( 'laptop' | 'notebook' | 'computador' <-> 'portatil' )", `+(laptop notebook "computador portatil")`},
		{"capa -celular", "'capa' & !( 'celular' | 'smartphone' )", "+capa -(celular smartphone)"},
		// MySQL drops the short alternative but keeps the others.
		{"tv", "( 'tv' | 'televisao' | 'televisor' )", "+(televisao televisor)"},
		// A phrase member does not trigger; a phrase is searched as written.
		{`"computador portatil"`, "'computador' <-> 'portatil'", `+"computador portatil"`},
	}
	for _, tt := range tests {
		v, err := synonymSearch.Search(tt.term).Value(pg)
		require.NoError(t, err, tt.term)
		assert.Equal(t, tt.pg, v, tt.term)
		v, err = synonymSearch.Search(tt.term).Value(my)
		require.NoError(t, err, tt.term)
		assert.Equal(t, tt.my, v, tt.term)
	}
	// Prefixes are not expanded: "note" is half a word.
	v, _ := synonymSearch.Search("notebook").Mode(fts.Prefix).Value(pg)
	assert.Equal(t, "'notebook':*", v)

	for _, bad := range [][][]string{{{"solo"}}, {{"a", "!!!"}}} {
		_, err := fts.NewIndex("products", fts.Spec{Name: "ftx_s", Columns: []fts.Column{{Name: "name"}}, Synonyms: bad})
		assert.ErrorIs(t, err, fts.ErrInvalidIndex, "%q", bad)
	}
}

func TestNear(t *testing.T) {
	pg, my := postgres.PostgresDialect{}, mysql.MySQLDialect{}
	v, err := products.Search(`"cafe coado"~2 moido`).Value(pg)
	require.NoError(t, err)
	assert.Equal(t, "( 'cafe' <1> 'coado' | 'coado' <1> 'cafe' | 'cafe' <2> 'coado' | 'coado' <2> 'cafe' ) & 'moido'", v)
	v, err = products.Search(`"cafe coado"~2 moido`).Value(my)
	require.NoError(t, err)
	assert.Equal(t, `+"cafe coado"@3 +moido`, v, "@N spans N words: N-1 positions apart")

	v, _ = products.Search(`-"cafe coado"~1` + " xicara").Value(pg)
	assert.Equal(t, "!( 'cafe' <1> 'coado' | 'coado' <1> 'cafe' ) & 'xicara'", v)

	v, _ = products.Search(`"cafe coado"~99`).Value(my)
	assert.Equal(t, `+"cafe coado"@11`, v, "capped at MaxNear")
	v, _ = products.Search(`"cafe bem coado"~3`).Value(pg)
	assert.Equal(t, "'cafe' <-> 'bem' <-> 'coado'", v, "proximity takes two words; more is a phrase")
	v, _ = products.Search(`"cafe de"~3`).Value(my)
	assert.Equal(t, `+"cafe de"`, v, "MySQL cannot measure distance to an unindexed word")
}

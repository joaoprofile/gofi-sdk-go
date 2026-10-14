package fts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The reference values are pg_trgm's similarity(a, b) on PostgreSQL 16:
// both databases must choose the suggestion PostgreSQL would.
func TestSimilarityMatchesPgTrgm(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want float64
	}{
		{"cafeteria", "cafeteira", 0.42857143},
		{"notbook", "notebook", 0.54545456},
		{"mause", "mouse", 0.33333334},
		{"eletrica", "eletrica", 1},
		{"cafe", "xicara", 0},
		{"tv", "televisao", 0.083333336},
		{"ab", "abc", 0.4},
	} {
		assert.InDelta(t, tt.want, similarity(tt.a, tt.b), 1e-6, "%s/%s", tt.a, tt.b)
	}
}

func TestReplaceWords(t *testing.T) {
	fix := map[string]string{"cafeteria": "cafeteira", "notbook": "notebook"}
	assert.Equal(t, `"cafeteira eletrica" -usado OR Notebook!`,
		replaceWords(`"Cafeteria eletrica" -usado OR Notbook!`, map[string]string{"cafeteria": "cafeteira", "notbook": "Notebook"}))
	assert.Equal(t, "cafeteira  e  notebook", replaceWords("cafetéria  e  notbook", fix), "matched without accents, spacing kept")
}

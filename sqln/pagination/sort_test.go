package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSort_SetsFieldAndDirection(t *testing.T) {
	s := NewSort("name", ASC)
	assert.Equal(t, "name", s.Field)
	assert.Equal(t, ASC, s.Direction)
}

func TestNewSort_DESC(t *testing.T) {
	s := NewSort("created_at", DESC)
	assert.Equal(t, "created_at", s.Field)
	assert.Equal(t, DESC, s.Direction)
}

func TestSortDirection_ASC_Value(t *testing.T) {
	assert.Equal(t, SortDirection("ASC"), ASC)
}

func TestSortDirection_DESC_Value(t *testing.T) {
	assert.Equal(t, SortDirection("DESC"), DESC)
}

func TestSort_JSONFields(t *testing.T) {
	s := Sort{Field: "price", Direction: DESC}
	assert.Equal(t, "price", s.Field)
	assert.Equal(t, DESC, s.Direction)
}

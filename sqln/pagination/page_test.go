package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPage_ZeroValue(t *testing.T) {
	var p Page[string]
	assert.Equal(t, uint64(0), p.TotalPages)
	assert.Equal(t, uint64(0), p.TotalElements)
	assert.Nil(t, p.Content)
}

func TestPage_WithContent(t *testing.T) {
	p := Page[int]{
		TotalPages:       3,
		TotalElements:    45,
		Size:             15,
		Number:           1,
		NumberOfElements: 15,
		Content:          []int{1, 2, 3},
	}
	assert.Equal(t, uint64(3), p.TotalPages)
	assert.Equal(t, uint64(45), p.TotalElements)
	assert.Len(t, p.Content, 3)
}

func TestPage_GenericString(t *testing.T) {
	p := Page[string]{Content: []string{"a", "b"}}
	assert.Equal(t, []string{"a", "b"}, p.Content)
}

func TestPage_GenericStruct(t *testing.T) {
	type Item struct{ ID int }
	p := Page[Item]{Content: []Item{{1}, {2}}}
	assert.Len(t, p.Content, 2)
	assert.Equal(t, 1, p.Content[0].ID)
}

package filter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var limited = Mapping{"id": {Column: "id"}, "name": {Column: "name"}}

// Regression: filter requests were unbounded (DoS through huge IN lists,
// thousands of conditions or long LIKE patterns).
func TestBuild_MaxFilters(t *testing.T) {
	fs := NewFilters()
	for range DefaultMaxFilters {
		fs.Add(NewFilter("id", Eq, 1))
	}
	_, err := Build("SELECT 1 WHERE 1=1", nil, fs, limited, pg)
	require.NoError(t, err)

	fs.Add(NewFilter("id", Eq, 1))
	_, err = Build("SELECT 1 WHERE 1=1", nil, fs, limited, pg)
	assert.ErrorIs(t, err, ErrInvalidFilter)

	_, err = Build("SELECT 1 WHERE 1=1", nil, fs, limited, pg, WithMaxFilters(DefaultMaxFilters+1))
	assert.NoError(t, err)
}

func TestBuild_MaxInValues(t *testing.T) {
	values := make([]any, DefaultMaxInValues+1)
	for i := range values {
		values[i] = i
	}
	_, err := Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("id", In, values)), limited, pg)
	assert.ErrorIs(t, err, ErrInvalidFilter)

	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("id", In, values[:DefaultMaxInValues])), limited, pg)
	assert.NoError(t, err)

	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("id", In, values[:3])), limited, pg, WithMaxInValues(2))
	assert.ErrorIs(t, err, ErrInvalidFilter)
}

func TestBuild_MaxLikeLength(t *testing.T) {
	long := strings.Repeat("é", DefaultMaxLikeLength+1)
	_, err := Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("name", Contains, long)), limited, pg)
	assert.ErrorIs(t, err, ErrInvalidFilter)

	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("name", Contains, long[:len(long)-2])), limited, pg)
	assert.NoError(t, err, "the bound counts characters, not bytes")

	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("name", Eq, long)), limited, pg)
	assert.NoError(t, err, "only LIKE values are bounded")
}

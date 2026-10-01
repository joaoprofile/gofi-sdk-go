package pagination

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Regression: limit came straight from the client (up to 65535 rows a page).
func TestNewPageRequestFromParams_ClampsLimit(t *testing.T) {
	assert.Equal(t, DefaultMaxLimit, NewPageRequestFromParams(0, 65535, "", "").Limit)
	assert.Equal(t, DefaultLimit, NewPageRequestFromParams(0, 0, "", "").Limit)
	assert.Equal(t, uint16(40), NewPageRequestFromParams(0, 40, "", "").Limit)
	assert.Equal(t, uint16(500), NewPageRequestFromParams(0, 500, "", "", WithMaxLimit(1000)).Limit)
	assert.Equal(t, uint16(10), NewPageRequestFromParams(0, 500, "", "", WithMaxLimit(10)).Limit)
}

func TestClampLimit(t *testing.T) {
	assert.Equal(t, DefaultMaxLimit, ClampLimit(DefaultMaxLimit+1))
	assert.Equal(t, DefaultMaxLimit, ClampLimit(1000, WithMaxLimit(0)), "0 keeps the default")
}

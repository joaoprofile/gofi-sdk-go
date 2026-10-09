package netx

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRequestID(t *testing.T) {
	assert.Empty(t, GetRequestID(context.Background()))
	assert.Equal(t, "abc", GetRequestID(context.WithValue(context.Background(), RequestIDKey, "abc")))
}

func TestValidRequestID(t *testing.T) {
	for _, id := range []string{"a", "req-123", "A.b_C-9", strings.Repeat("x", maxRequestIDLen)} {
		assert.True(t, ValidRequestID(id), id)
	}
	for _, id := range []string{"", "has space", "new\nline", "ação", strings.Repeat("x", maxRequestIDLen+1)} {
		assert.False(t, ValidRequestID(id), id)
	}
}

func TestNewRequestID(t *testing.T) {
	a, b := NewRequestID(), NewRequestID()
	assert.True(t, ValidRequestID(a))
	assert.NotEqual(t, a, b)
}

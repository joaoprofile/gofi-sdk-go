package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type plainDialect struct{}

func (plainDialect) Param(int) string           { return "?" }
func (plainDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (plainDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }
func (bracketDialect) LikeWildcards() string    { return "[" }

type bracketDialect struct{ plainDialect }

func TestEscapeLike(t *testing.T) {
	assert.Equal(t, `a!%b!_c!!d\e[f`, EscapeLike(plainDialect{}, `a%b_c!d\e[f`))
	assert.Equal(t, `a!%b!_c!!d\e![f`, EscapeLike(bracketDialect{}, `a%b_c!d\e[f`))
	assert.Equal(t, "joão", EscapeLike(plainDialect{}, "joão"))
	assert.Equal(t, " ESCAPE '!'", LikeEscapeClause)
}

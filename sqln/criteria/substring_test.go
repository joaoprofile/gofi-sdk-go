package criteria_test

import (
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/sqlserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstring_EscapesAndDeclaresEscape(t *testing.T) {
	clause, params := built(t)(criteria.BuildClause([]criteria.Predicate{
		criteria.Contains("name", "50%_off").Substring(),
	}, pg))
	assert.Equal(t, "name ILIKE $1 ESCAPE '!'", clause)
	assert.Equal(t, []any{"%50!%!_off%"}, params)

	clause, params = built(t)(criteria.BuildClause([]criteria.Predicate{
		criteria.NotLike("code", "[a]").Substring(),
	}, sqlserver.SQLServerDialect{}))
	assert.Equal(t, "code NOT LIKE @p1 ESCAPE '!'", clause)
	assert.Equal(t, []any{"%![a]%"}, params, "T-SQL [ classes are escaped")
}

func TestSubstring_WithoutItPatternsStayPatterns(t *testing.T) {
	clause, params := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.Like("code", "A_%")}, pg))
	assert.Equal(t, "code LIKE $1", clause)
	assert.Equal(t, []any{"A_%"}, params)
}

func TestSubstring_RequiresString(t *testing.T) {
	_, _, err := criteria.BuildClause([]criteria.Predicate{criteria.Predicate{}.Substring()}, pg)
	require.Error(t, err)
}

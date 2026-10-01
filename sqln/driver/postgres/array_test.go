package postgres

import (
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/stretchr/testify/assert"
)

func TestInBindsTypedSliceAsArray(t *testing.T) {
	sql, params := built(t)(criteria.BuildClause([]criteria.Predicate{
		criteria.In("region", []string{"br", "us"}),
		criteria.NotIn("id", []int64{1, 2, 3}),
	}, PostgresDialect{}))
	assert.Equal(t, "region = ANY($1) AND id <> ALL($2)", sql)
	assert.Equal(t, []any{[]string{"br", "us"}, []int64{1, 2, 3}}, params)
}

func TestInExpandsMixedSlice(t *testing.T) {
	sql, params := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.In("x", []any{1, "a"})}, PostgresDialect{}))
	assert.Equal(t, "x IN ($1, $2)", sql)
	assert.Len(t, params, 2)
}

func TestInEmptySliceStillMatchesNothing(t *testing.T) {
	sql, _ := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.In("x", []string{})}, PostgresDialect{}))
	assert.Equal(t, "1 = 0", sql)
}

// built fails the test on a build error and returns the SQL and its parameters.
func built(t testing.TB) func(string, []any, error) (string, []any) {
	return func(sql string, params []any, err error) (string, []any) {
		t.Helper()
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return sql, params
	}
}

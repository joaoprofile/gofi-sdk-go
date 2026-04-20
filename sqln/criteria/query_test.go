package criteria_test

// Additional unit tests for query.go structural and edge-case behaviour.
// Scenario coverage for Build/BuildBase is in criteria_test.go.

import (
	"strings"
	"testing"

	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/stretchr/testify/assert"
)

// From

func TestFrom_ReturnsNonNilPointer(t *testing.T) {
	q := criteria.From("users", "u")
	assert.NotNil(t, q)
}

func TestFrom_WithoutAlias_ProducesNoAlias(t *testing.T) {
	sql, _ := criteria.From("users", "").Build(pgDialect{})
	assert.Equal(t, "SELECT * FROM users", sql)
	// Alias should not appear.
	assert.NotContains(t, sql, "  ") // no double space either
}

func TestFrom_TableNamePreservedExactly(t *testing.T) {
	sql, _ := criteria.From("schema.users", "u").Build(pgDialect{})
	assert.Contains(t, sql, "FROM schema.users")
}

// Select

func TestSelect_SingleCall_MultipleFields(t *testing.T) {
	sql, _ := criteria.From("t", "").Select("a", "b", "c").Build(pgDialect{})
	assert.Equal(t, "SELECT a, b, c FROM t", sql)
}

func TestSelect_FallsBackToStarWhenNeverCalled(t *testing.T) {
	sql, _ := criteria.From("t", "").Build(pgDialect{})
	assert.True(t, strings.HasPrefix(sql, "SELECT *"))
}

//  Limit / Offset

func TestLimit_CalledTwice_LastValueWins(t *testing.T) {
	sql, _ := criteria.From("t", "").Limit(5).Limit(20).Build(pgDialect{})
	assert.Contains(t, sql, "LIMIT 20")
	assert.NotContains(t, sql, "LIMIT 5")
}

func TestOffset_CalledTwice_LastValueWins(t *testing.T) {
	sql, _ := criteria.From("t", "").Offset(10).Offset(30).Build(pgDialect{})
	assert.Contains(t, sql, "OFFSET 30")
	assert.NotContains(t, sql, "OFFSET 10")
}

func TestLimitOffset_OrderInSQL_LimitBeforeOffset(t *testing.T) {
	sql, _ := criteria.From("t", "").Limit(10).Offset(5).Build(pgDialect{})
	limitPos := indexOf(sql, "LIMIT")
	offsetPos := indexOf(sql, "OFFSET")
	assert.Less(t, limitPos, offsetPos)
}

// GroupBy

func TestGroupBy_CalledMultipleTimes_Accumulates(t *testing.T) {
	sql, _ := criteria.From("t", "").
		GroupBy("a").
		GroupBy("b", "c").
		Build(pgDialect{})
	assert.Contains(t, sql, "GROUP BY a, b, c")
}

func TestGroupBy_WithoutWhere_ProducesNoWhere(t *testing.T) {
	sql, _ := criteria.From("t", "").GroupBy("x").Build(pgDialect{})
	assert.NotContains(t, sql, "WHERE")
	assert.Contains(t, sql, "GROUP BY x")
}

// Having

func TestHaving_CalledMultipleTimes_Accumulates(t *testing.T) {
	sql, params := criteria.From("t", "").
		GroupBy("x").
		Having(criteria.Gt("COUNT(*)", 1)).
		Having(criteria.Lt("SUM(v)", 100)).
		Build(pgDialect{})
	assert.Contains(t, sql, "HAVING COUNT(*) > $1 AND SUM(v) < $2")
	assert.Equal(t, []any{1, 100}, params)
}

func TestHaving_WithoutGroupBy_StillEmitted(t *testing.T) {
	// SQL allows HAVING without GROUP BY (operates on the entire result as one group).
	sql, _ := criteria.From("t", "").Having(criteria.Gt("COUNT(*)", 0)).Build(pgDialect{})
	assert.Contains(t, sql, "HAVING COUNT(*) > $1")
}

// Where

func TestWhere_CalledThreeTimes_AllAccumulated(t *testing.T) {
	sql, params := criteria.From("t", "").
		Where(criteria.Eq("a", 1)).
		Where(criteria.Eq("b", 2)).
		Where(criteria.Eq("c", 3)).
		Build(pgDialect{})
	assert.Equal(t, "SELECT * FROM t WHERE a = $1 AND b = $2 AND c = $3", sql)
	assert.Equal(t, []any{1, 2, 3}, params)
}

func TestWhere_SinglePredicate_NoConnectorInSQL(t *testing.T) {
	sql, _ := criteria.From("t", "").Where(criteria.Eq("x", 1)).Build(pgDialect{})
	// Only one predicate — no AND or OR should appear.
	assert.NotContains(t, sql, " AND ")
	assert.NotContains(t, sql, " OR ")
}

// BuildBase strips trailing clauses

func TestBuildBase_NoOrderByInOutput(t *testing.T) {
	sql, _ := criteria.From("t", "").
		OrderBy(criteria.Asc("id"), criteria.Desc("name")).
		BuildBase(pgDialect{})
	assert.NotContains(t, sql, "ORDER BY")
}

func TestBuildBase_NoLimitInOutput(t *testing.T) {
	sql, _ := criteria.From("t", "").Limit(50).BuildBase(pgDialect{})
	assert.NotContains(t, sql, "LIMIT")
}

func TestBuildBase_NoOffsetInOutput(t *testing.T) {
	sql, _ := criteria.From("t", "").Offset(10).BuildBase(pgDialect{})
	assert.NotContains(t, sql, "OFFSET")
}

func TestBuildBase_ParamsMatch_Build(t *testing.T) {
	q := criteria.From("t", "").
		Where(criteria.Eq("a", 1), criteria.In("b", []int{2, 3})).
		OrderBy(criteria.Asc("a")).
		Limit(10)
	_, paramsFull := q.Build(pgDialect{})
	_, paramsBase := q.BuildBase(pgDialect{})
	// Same params — ORDER BY and LIMIT don't bind parameters.
	assert.Equal(t, paramsFull, paramsBase)
}

// Clause ordering in SQL

func TestBuild_ClauseOrder_WhereBeforeGroupBy(t *testing.T) {
	sql, _ := criteria.From("t", "").
		GroupBy("x").
		Where(criteria.Eq("a", 1)).
		Build(pgDialect{})
	wherePos := indexOf(sql, "WHERE")
	groupPos := indexOf(sql, "GROUP BY")
	assert.Less(t, wherePos, groupPos)
}

func TestBuild_ClauseOrder_GroupByBeforeHaving(t *testing.T) {
	sql, _ := criteria.From("t", "").
		GroupBy("x").
		Having(criteria.Gt("COUNT(*)", 1)).
		Build(pgDialect{})
	groupPos := indexOf(sql, "GROUP BY")
	havingPos := indexOf(sql, "HAVING")
	assert.Less(t, groupPos, havingPos)
}

func TestBuild_ClauseOrder_HavingBeforeOrderBy(t *testing.T) {
	sql, _ := criteria.From("t", "").
		GroupBy("x").
		Having(criteria.Gt("COUNT(*)", 1)).
		OrderBy(criteria.Asc("x")).
		Build(pgDialect{})
	havingPos := indexOf(sql, "HAVING")
	orderPos := indexOf(sql, "ORDER BY")
	assert.Less(t, havingPos, orderPos)
}

func TestBuild_ClauseOrder_OrderByBeforeLimit(t *testing.T) {
	sql, _ := criteria.From("t", "").
		OrderBy(criteria.Asc("id")).
		Limit(10).
		Build(pgDialect{})
	orderPos := indexOf(sql, "ORDER BY")
	limitPos := indexOf(sql, "LIMIT")
	assert.Less(t, orderPos, limitPos)
}

//  Build returns empty params when no predicates

func TestBuild_NoPredicates_EmptyParams(t *testing.T) {
	_, params := criteria.From("t", "").
		OrderBy(criteria.Asc("id")).
		Limit(10).
		Build(pgDialect{})
	assert.Empty(t, params)
}

//  Multiple JOINs preserve order

func TestBuild_JoinOrder_PreservedInSQL(t *testing.T) {
	sql, _ := criteria.From("a", "").
		Join("b", "bb", "bb.a_id = a.id").
		LeftJoin("c", "cc", "cc.b_id = bb.id").
		RightJoin("d", "dd", "dd.c_id = cc.id").
		Build(pgDialect{})
	bPos := indexOf(sql, "JOIN b")
	cPos := indexOf(sql, "LEFT JOIN c")
	dPos := indexOf(sql, "RIGHT JOIN d")
	assert.Less(t, bPos, cPos)
	assert.Less(t, cPos, dPos)
}

//  Helpers

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

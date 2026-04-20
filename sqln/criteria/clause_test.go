package criteria_test

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/stretchr/testify/assert"
)

// Dialects (pgDialect, myDialect, oraDialect, mssDialect) are declared in criteria_test.go
// and shared across the criteria_test package.

// BuildClause — empty input

func TestBuildClause_Empty_ReturnsEmptyStringAndNilParams(t *testing.T) {
	clause, params := criteria.BuildClause(nil, pg)
	assert.Equal(t, "", clause)
	assert.Empty(t, params)
}

func TestBuildClause_EmptySlice_ReturnsEmptyStringAndNilParams(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{}, pg)
	assert.Equal(t, "", clause)
	assert.Empty(t, params)
}

// BuildClause — single predicate

func TestBuildClause_SingleEq(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("status", "active"),
	}, pg)
	assert.Equal(t, "status = $1", clause)
	assert.Equal(t, []any{"active"}, params)
}

func TestBuildClause_SingleIsNull(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.IsNull("deleted_at"),
	}, pg)
	assert.Equal(t, "deleted_at IS NULL", clause)
	assert.Empty(t, params)
}

func TestBuildClause_SingleIsNotNull(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.IsNotNull("confirmed_at"),
	}, pg)
	assert.Equal(t, "confirmed_at IS NOT NULL", clause)
	assert.Empty(t, params)
}

func TestBuildClause_SingleIn(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.In("role", []string{"admin", "mod"}),
	}, pg)
	assert.Equal(t, "role IN ($1, $2)", clause)
	assert.Equal(t, []any{"admin", "mod"}, params)
}

func TestBuildClause_SingleBetween(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Between("created_at", from, to),
	}, pg)
	assert.Equal(t, "created_at BETWEEN $1 AND $2", clause)
	assert.Equal(t, []any{from, to}, params)
}

// BuildClause — multiple predicates

func TestBuildClause_TwoPredicates_ImplicitAND(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.Eq("b", 2),
	}, pg)
	assert.Equal(t, "a = $1 AND b = $2", clause)
	assert.Equal(t, []any{1, 2}, params)
}

func TestBuildClause_ExplicitAND(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.And(),
		criteria.Eq("b", 2),
	}, pg)
	assert.Equal(t, "a = $1 AND b = $2", clause)
	assert.Equal(t, []any{1, 2}, params)
}

func TestBuildClause_ExplicitOR(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("type", "A"),
		criteria.Or(),
		criteria.Eq("type", "B"),
	}, pg)
	assert.Equal(t, "type = $1 OR type = $2", clause)
	assert.Equal(t, []any{"A", "B"}, params)
}

func TestBuildClause_MixedLogical(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.And(),
		criteria.Eq("b", 2),
		criteria.Or(),
		criteria.Eq("c", 3),
	}, pg)
	assert.Equal(t, "a = $1 AND b = $2 OR c = $3", clause)
	assert.Equal(t, []any{1, 2, 3}, params)
}

// BuildClause — Group predicate

func TestBuildClause_Group_WrapsInParentheses(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("tenant_id", 10),
		criteria.And(),
		criteria.Group(
			criteria.Eq("role", "admin"),
			criteria.Or(),
			criteria.Eq("role", "mod"),
		),
	}, pg)
	assert.Equal(t, "tenant_id = $1 AND (role = $2 OR role = $3)", clause)
	assert.Equal(t, []any{10, "admin", "mod"}, params)
}

// BuildClause — text search

func TestBuildClause_Contains_PostgreSQL_UsesILIKE(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Contains("name", "%john%"),
	}, pg)
	assert.Equal(t, "name ILIKE $1", clause)
	assert.Equal(t, []any{"%john%"}, params)
}

func TestBuildClause_Contains_MySQL_UsesLIKE(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Contains("name", "%john%"),
	}, my)
	assert.Equal(t, "name LIKE ?", clause)
	assert.Equal(t, []any{"%john%"}, params)
}

func TestBuildClause_NotContains_PostgreSQL(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.NotContains("name", "%spam%"),
	}, pg)
	assert.Equal(t, "name NOT ILIKE $1", clause)
	assert.Equal(t, []any{"%spam%"}, params)
}

// BuildClause — dialect placeholder formats

func TestBuildClause_MySQL_Placeholders(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.And(),
		criteria.Eq("b", "x"),
	}, my)
	assert.Equal(t, "a = ? AND b = ?", clause)
	assert.Equal(t, []any{1, "x"}, params)
}

func TestBuildClause_Oracle_Placeholders(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.And(),
		criteria.Eq("b", "x"),
	}, ora)
	assert.Equal(t, "a = :1 AND b = :2", clause)
	assert.Equal(t, []any{1, "x"}, params)
}

func TestBuildClause_SQLServer_Placeholders(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.Eq("a", 1),
		criteria.And(),
		criteria.Eq("b", "x"),
	}, mss)
	assert.Equal(t, "a = @p1 AND b = @p2", clause)
	assert.Equal(t, []any{1, "x"}, params)
}

// BuildClause — parameter continuity (params must be numbered sequentially)

func TestBuildClause_ParameterContinuity_AcrossPredicates(t *testing.T) {
	clause, params := criteria.BuildClause([]criteria.Predicate{
		criteria.In("role", []string{"a", "b"}),
		criteria.And(),
		criteria.Eq("active", true),
	}, pg)
	assert.Equal(t, "role IN ($1, $2) AND active = $3", clause)
	assert.Equal(t, []any{"a", "b", true}, params)
}

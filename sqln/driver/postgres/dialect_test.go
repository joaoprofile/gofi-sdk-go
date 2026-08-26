package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var dialect = PostgresDialect{}

// ---------------------------------------------------------------------------
// Param
// ---------------------------------------------------------------------------

func TestParam_Index1_ReturnsDollar1(t *testing.T) {
	assert.Equal(t, "$1", dialect.Param(1))
}

func TestParam_Index5_ReturnsDollar5(t *testing.T) {
	assert.Equal(t, "$5", dialect.Param(5))
}

func TestParam_Index100_ReturnsDollar100(t *testing.T) {
	assert.Equal(t, "$100", dialect.Param(100))
}

// ---------------------------------------------------------------------------
// Like
// ---------------------------------------------------------------------------

func TestLike_UsesILIKE(t *testing.T) {
	assert.Equal(t, "name ILIKE $1", dialect.Like("name", "$1"))
}

func TestLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email ILIKE $3", dialect.Like("email", "$3"))
}

// ---------------------------------------------------------------------------
// NotLike
// ---------------------------------------------------------------------------

func TestNotLike_UsesNotILIKE(t *testing.T) {
	assert.Equal(t, "name NOT ILIKE $1", dialect.NotLike("name", "$1"))
}

func TestNotLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email NOT ILIKE $5", dialect.NotLike("email", "$5"))
}

// ---------------------------------------------------------------------------
// BuildPagination
// ---------------------------------------------------------------------------

func TestBuildPagination_BasicQuery(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 0)
	assert.Equal(t, "SELECT id FROM t ORDER BY id ASC LIMIT 10 OFFSET 0", result)
}

func TestBuildPagination_WithOffset(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "name DESC", 15, 30)
	assert.Equal(t, "SELECT id FROM t ORDER BY name DESC LIMIT 15 OFFSET 30", result)
}

func TestBuildPagination_MultiColumnOrder(t *testing.T) {
	result := dialect.BuildPagination("SELECT * FROM users", "name ASC, age DESC", 5, 10)
	assert.Equal(t, "SELECT * FROM users ORDER BY name ASC, age DESC LIMIT 5 OFFSET 10", result)
}

// ---------------------------------------------------------------------------
// BuildCount
// ---------------------------------------------------------------------------

func TestBuildCount_WrapsWithCountStar(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM t")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM t) tb", result)
}

func TestBuildCount_ComplexSubquery(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM orders WHERE status = $1")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM orders WHERE status = $1) tb", result)
}

// Regression: COUNT(tb.*) names the composite row, so the planner materializes
// it and stops eliminating unprojected unique-key LEFT JOINs — measured in
// production as 11ms turning into 180ms on the same query.
func TestBuildCount_DoesNotReferenceCompositeRow(t *testing.T) {
	result := dialect.BuildCount("SELECT a.id FROM a LEFT JOIN b ON b.a_id = a.id")
	assert.NotContains(t, result, "COUNT(tb.*)")
	assert.Contains(t, result, "COUNT(*)")
}

// Regression: offset is page*limit and used to be uint16, wrapping past 65535.
func TestBuildPagination_DeepOffset_BeyondUint16(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 15, 66000)
	assert.Equal(t, "SELECT id FROM t ORDER BY id ASC LIMIT 15 OFFSET 66000", result)
}

package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var dialect = MySQLDialect{}

// ---------------------------------------------------------------------------
// Param
// ---------------------------------------------------------------------------

func TestParam_AlwaysReturnsQuestionMark(t *testing.T) {
	assert.Equal(t, "?", dialect.Param(1))
	assert.Equal(t, "?", dialect.Param(5))
	assert.Equal(t, "?", dialect.Param(100))
}

// ---------------------------------------------------------------------------
// Like
// ---------------------------------------------------------------------------

func TestLike_UsesLIKE(t *testing.T) {
	assert.Equal(t, "name LIKE ?", dialect.Like("name", "?"))
}

func TestLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email LIKE ?", dialect.Like("email", "?"))
}

// ---------------------------------------------------------------------------
// NotLike
// ---------------------------------------------------------------------------

func TestNotLike_UsesNotLIKE(t *testing.T) {
	assert.Equal(t, "name NOT LIKE ?", dialect.NotLike("name", "?"))
}

func TestNotLike_DifferentField(t *testing.T) {
	assert.Equal(t, "email NOT LIKE ?", dialect.NotLike("email", "?"))
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
	result := dialect.BuildCount("SELECT id FROM orders WHERE status = ?")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM orders WHERE status = ?) tb", result)
}

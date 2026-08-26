package sqlserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var dialect = SQLServerDialect{}

// ---------------------------------------------------------------------------
// Param
// ---------------------------------------------------------------------------

func TestParam_Index1_ReturnsAtP1(t *testing.T) {
	assert.Equal(t, "@p1", dialect.Param(1))
}

func TestParam_Index7_ReturnsAtP7(t *testing.T) {
	assert.Equal(t, "@p7", dialect.Param(7))
}

func TestParam_Index100_ReturnsAtP100(t *testing.T) {
	assert.Equal(t, "@p100", dialect.Param(100))
}

// ---------------------------------------------------------------------------
// Like
// ---------------------------------------------------------------------------

func TestLike_UsesLIKE(t *testing.T) {
	assert.Equal(t, "name LIKE @p1", dialect.Like("name", "@p1"))
}

func TestLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email LIKE @p2", dialect.Like("email", "@p2"))
}

// ---------------------------------------------------------------------------
// NotLike
// ---------------------------------------------------------------------------

func TestNotLike_UsesNotLIKE(t *testing.T) {
	assert.Equal(t, "name NOT LIKE @p1", dialect.NotLike("name", "@p1"))
}

func TestNotLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email NOT LIKE @p3", dialect.NotLike("email", "@p3"))
}

// ---------------------------------------------------------------------------
// BuildPagination
// ---------------------------------------------------------------------------

func TestBuildPagination_BasicQuery(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 0)
	assert.Equal(t, "SELECT id FROM t ORDER BY id ASC OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY", result)
}

func TestBuildPagination_WithOffset(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "name DESC", 15, 30)
	assert.Equal(t, "SELECT id FROM t ORDER BY name DESC OFFSET 30 ROWS FETCH NEXT 15 ROWS ONLY", result)
}

func TestBuildPagination_MultiColumnOrder(t *testing.T) {
	result := dialect.BuildPagination("SELECT * FROM users", "name ASC, age DESC", 5, 10)
	assert.Equal(t, "SELECT * FROM users ORDER BY name ASC, age DESC OFFSET 10 ROWS FETCH NEXT 5 ROWS ONLY", result)
}

// ---------------------------------------------------------------------------
// BuildCount
// ---------------------------------------------------------------------------

func TestBuildCount_WrapsWithCountStarAndAlias(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM t")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM t) AS tb", result)
}

func TestBuildCount_ComplexSubquery(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM orders WHERE status = @p1")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM orders WHERE status = @p1) AS tb", result)
}

// Regression: offset is page*limit and used to be uint16, wrapping past 65535.
func TestBuildPagination_DeepOffset_BeyondUint16(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 15, 66000)
	assert.Contains(t, result, "OFFSET 66000 ROWS")
	assert.Contains(t, result, "FETCH NEXT 15 ROWS ONLY")
}

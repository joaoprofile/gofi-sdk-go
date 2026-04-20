package oracle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var dialect = OracleDialect{}

// ---------------------------------------------------------------------------
// Param
// ---------------------------------------------------------------------------

func TestParam_Index1_ReturnsColon1(t *testing.T) {
	assert.Equal(t, ":1", dialect.Param(1))
}

func TestParam_Index3_ReturnsColon3(t *testing.T) {
	assert.Equal(t, ":3", dialect.Param(3))
}

func TestParam_Index10_ReturnsColon10(t *testing.T) {
	assert.Equal(t, ":10", dialect.Param(10))
}

// ---------------------------------------------------------------------------
// Like
// ---------------------------------------------------------------------------

func TestLike_UsesLIKE(t *testing.T) {
	assert.Equal(t, "name LIKE :1", dialect.Like("name", ":1"))
}

func TestLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email LIKE :2", dialect.Like("email", ":2"))
}

// ---------------------------------------------------------------------------
// NotLike
// ---------------------------------------------------------------------------

func TestNotLike_UsesNotLIKE(t *testing.T) {
	assert.Equal(t, "name NOT LIKE :1", dialect.NotLike("name", ":1"))
}

func TestNotLike_DifferentFieldAndParam(t *testing.T) {
	assert.Equal(t, "email NOT LIKE :4", dialect.NotLike("email", ":4"))
}

// ---------------------------------------------------------------------------
// BuildPagination
// ---------------------------------------------------------------------------

func TestBuildPagination_ContainsROWNUM(t *testing.T) {
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 0)
	assert.Contains(t, result, "ROWNUM")
	assert.Contains(t, result, "SELECT id FROM t")
	assert.Contains(t, result, "id ASC")
}

func TestBuildPagination_OffsetZero_ROWNUMLimitEqualsLimit(t *testing.T) {
	// offset=0, limit=10 → ROWNUM <= 10, rn > 0
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 0)
	assert.Contains(t, result, "ROWNUM <= 10")
	assert.Contains(t, result, "rn > 0")
}

func TestBuildPagination_WithOffset_ROWNUMCorrect(t *testing.T) {
	// offset=20, limit=10 → ROWNUM <= 30, rn > 20
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 20)
	assert.Contains(t, result, "ROWNUM <= 30")
	assert.Contains(t, result, "rn > 20")
}

func TestBuildPagination_FullStructure(t *testing.T) {
	expected := "SELECT * FROM (SELECT tb.*, ROWNUM rn FROM (SELECT id FROM t ORDER BY id ASC) tb WHERE ROWNUM <= 10) WHERE rn > 0"
	result := dialect.BuildPagination("SELECT id FROM t", "id ASC", 10, 0)
	assert.Equal(t, expected, result)
}

// ---------------------------------------------------------------------------
// BuildCount
// ---------------------------------------------------------------------------

func TestBuildCount_WrapsWithCountStar(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM t")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM t) tb", result)
}

func TestBuildCount_ComplexSubquery(t *testing.T) {
	result := dialect.BuildCount("SELECT id FROM orders WHERE status = :1")
	assert.Equal(t, "SELECT COUNT(*) FROM (SELECT id FROM orders WHERE status = :1) tb", result)
}

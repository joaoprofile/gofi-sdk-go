package filter

import (
	"context"
	"database/sql"
	stdsqldriver "database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
)

// Test Dialects

// pgDialect mirrors PostgreSQL behaviour: $N placeholders and ILIKE.
type pgDialect struct{}

func (pgDialect) Param(i int) string         { return fmt.Sprintf("$%d", i) }
func (pgDialect) Like(f, p string) string    { return fmt.Sprintf("%s ILIKE %s", f, p) }
func (pgDialect) NotLike(f, p string) string { return fmt.Sprintf("%s NOT ILIKE %s", f, p) }

// myDialect mirrors MySQL behaviour: ? placeholders and LIKE.
type myDialect struct{}

func (myDialect) Param(_ int) string         { return "?" }
func (myDialect) Like(f, p string) string    { return fmt.Sprintf("%s LIKE %s", f, p) }
func (myDialect) NotLike(f, p string) string { return fmt.Sprintf("%s NOT LIKE %s", f, p) }

// mssqlDialect mirrors SQL Server: @pN placeholders and LIKE.
type mssqlDialect struct{}

func (mssqlDialect) Param(i int) string         { return fmt.Sprintf("@p%d", i) }
func (mssqlDialect) Like(f, p string) string    { return fmt.Sprintf("%s LIKE %s", f, p) }
func (mssqlDialect) NotLike(f, p string) string { return fmt.Sprintf("%s NOT LIKE %s", f, p) }

// oraDialect mirrors Oracle: :N placeholders and LIKE.
type oraDialect struct{}

func (oraDialect) Param(i int) string         { return fmt.Sprintf(":%d", i) }
func (oraDialect) Like(f, p string) string    { return fmt.Sprintf("%s LIKE %s", f, p) }
func (oraDialect) NotLike(f, p string) string { return fmt.Sprintf("%s NOT LIKE %s", f, p) }

var (
	pg    = pgDialect{}
	my    = myDialect{}
	mssql = mssqlDialect{}
	ora   = oraDialect{}
)

const base = "SELECT * FROM t WHERE 1=1"

// Constructors─

func TestNewFilters_Defaults(t *testing.T) {
	f := NewFilters()
	assert.NotNil(t, f)
	assert.Empty(t, f.Filters)
	assert.Equal(t, defaultPage, f.Params.Page)
	assert.Equal(t, defaultLimit, f.Params.Limit)
	assert.Equal(t, defaultSortDirection, f.Params.SortDirection)
}

func TestNewFilter_Fields(t *testing.T) {
	f := NewFilter("name", Eq, "Emilia")
	assert.Equal(t, "name", f.Field)
	assert.Equal(t, Eq, f.Condition)
	assert.Equal(t, "Emilia", f.Value)
}

func TestAND_LogicalOperator(t *testing.T) {
	assert.Equal(t, And, AND().LogicalOperator)
}

func TestOR_LogicalOperator(t *testing.T) {
	assert.Equal(t, Or, OR().LogicalOperator)
}

func TestFilters_Add_MultipleFilters(t *testing.T) {
	f1 := NewFilter("a", Eq, 1)
	f2 := NewFilter("b", Eq, 2)
	fs := NewFilters().Add(f1, f2)
	assert.Len(t, fs.Filters, 2)
}

// Empty Filters

func TestQueryBuild_NoFilters_ReturnsBaseQuery(t *testing.T) {
	qp := NewQueryBuildWithDialect(base, NewFilters(), pg)
	assert.Equal(t, base, qp.Query)
	assert.Empty(t, qp.Params)
}

// Comparison Operators

func TestQueryBuild_Eq_String(t *testing.T) {
	fs := NewFilters().Add(NewFilter("status", Eq, "active"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( status = $1 )", qp.Query)
	assert.Equal(t, []any{StringValue("active")}, qp.Params)
}

func TestQueryBuild_Eq_Int(t *testing.T) {
	fs := NewFilters().Add(NewFilter("age", Eq, 30))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( age = $1 )", qp.Query)
	assert.Equal(t, []any{IntValue(30)}, qp.Params)
}

func TestQueryBuild_Eq_Float(t *testing.T) {
	fs := NewFilters().Add(NewFilter("price", Eq, 9.99))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( price = $1 )", qp.Query)
	assert.Equal(t, []any{FloatValue(9.99)}, qp.Params)
}

func TestQueryBuild_NotEqual_Scalar(t *testing.T) {
	fs := NewFilters().Add(NewFilter("status", NotEqual, "inactive"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( status != $1 )", qp.Query)
}

func TestQueryBuild_Less(t *testing.T) {
	fs := NewFilters().Add(NewFilter("age", Less, 18))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( age < $1 )", qp.Query)
}

func TestQueryBuild_LessOrEqual(t *testing.T) {
	fs := NewFilters().Add(NewFilter("age", LessOrEqual, 18))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( age <= $1 )", qp.Query)
}

func TestQueryBuild_Greater(t *testing.T) {
	fs := NewFilters().Add(NewFilter("price", Greater, 100.0))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( price > $1 )", qp.Query)
}

func TestQueryBuild_GreaterOrEqual(t *testing.T) {
	fs := NewFilters().Add(NewFilter("price", GreaterOrEqual, 100))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( price >= $1 )", qp.Query)
}

// Membership Operators

func TestQueryBuild_In_MultipleValues(t *testing.T) {
	fs := NewFilters().Add(NewFilter("status", In, []any{"a", "b", "c"}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( status IN ($1, $2, $3) )", qp.Query)
	assert.Len(t, qp.Params, 3)
}

func TestQueryBuild_NotIn_MultipleValues(t *testing.T) {
	fs := NewFilters().Add(NewFilter("tag", NotIn, []any{"x", "y"}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( tag NOT IN ($1, $2) )", qp.Query)
	assert.Len(t, qp.Params, 2)
}

// Eq with slice → IN (semantic alias)
func TestQueryBuild_Eq_Slice_ProducesIN(t *testing.T) {
	fs := NewFilters().Add(NewFilter("id", Eq, []any{1, 2, 3}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( id IN ($1, $2, $3) )", qp.Query)
}

// != with slice → NOT IN
func TestQueryBuild_NotEqual_Slice_ProducesNotIN(t *testing.T) {
	fs := NewFilters().Add(NewFilter("highlight", NotEqual, []any{"NON_CATALOG", "SPONSORED"}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( highlight NOT IN ($1, $2) )", qp.Query)
}

// Single-element slice treated as scalar
func TestQueryBuild_SingleElementSlice_TreatedAsScalar(t *testing.T) {
	fs := NewFilters().Add(NewFilter("marketplace_id", Eq, []any{2}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( marketplace_id = $1 )", qp.Query)
	assert.Len(t, qp.Params, 1)
}

// Text Search Operators

func TestQueryBuild_Contains_PostgreSQL_UsesILIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", Contains, "joão"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( name ILIKE $1 )", qp.Query)
	assert.Equal(t, []any{"%joão%"}, qp.Params)
}

func TestQueryBuild_Contains_MySQL_UsesLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", Contains, "maria"))
	qp := NewQueryBuildWithDialect(base, fs, my)
	assert.Equal(t, base+" AND ( name LIKE ? )", qp.Query)
	assert.Equal(t, []any{"%maria%"}, qp.Params)
}

func TestQueryBuild_Contains_SQLServer_UsesLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", Contains, "ana"))
	qp := NewQueryBuildWithDialect(base, fs, mssql)
	assert.Equal(t, base+" AND ( name LIKE @p1 )", qp.Query)
}

func TestQueryBuild_Contains_Oracle_UsesLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", Contains, "pedro"))
	qp := NewQueryBuildWithDialect(base, fs, ora)
	assert.Equal(t, base+" AND ( name LIKE :1 )", qp.Query)
}

func TestQueryBuild_NotContains_PostgreSQL_UsesNotILIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", NotContains, "bot"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( name NOT ILIKE $1 )", qp.Query)
	assert.Equal(t, []any{"%bot%"}, qp.Params)
}

func TestQueryBuild_NotContains_MySQL_UsesNotLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("name", NotContains, "bot"))
	qp := NewQueryBuildWithDialect(base, fs, my)
	assert.Equal(t, base+" AND ( name NOT LIKE ? )", qp.Query)
}

func TestQueryBuild_Like_AlwaysCaseSensitiveLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("code", Like, "MLB"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( code LIKE $1 )", qp.Query)
	assert.Equal(t, []any{"%MLB%"}, qp.Params)
}

func TestQueryBuild_Like_MySQL_AlsoCaseSensitiveLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("code", Like, "MLB"))
	qp := NewQueryBuildWithDialect(base, fs, my)
	assert.Equal(t, base+" AND ( code LIKE ? )", qp.Query)
}

func TestQueryBuild_NotLike_AlwaysCaseSensitiveNotLIKE(t *testing.T) {
	fs := NewFilters().Add(NewFilter("code", NotLike, "TMP"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( code NOT LIKE $1 )", qp.Query)
	assert.Equal(t, []any{"%TMP%"}, qp.Params)
}

// Range Operator

func TestQueryBuild_Between_StringEncoded(t *testing.T) {
	fs := NewFilters().Add(NewFilter("created_at", Between, "2024-01-01T00:00:00Z|2024-12-31T23:59:59Z"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( created_at BETWEEN $1 AND $2 )", qp.Query)
	assert.Len(t, qp.Params, 2)
	_, ok := qp.Params[0].(time.Time)
	assert.True(t, ok, "param[0] should be time.Time")
}

func TestQueryBuild_Between_TimeSliceDirect(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	fs := NewFilters().Add(NewFilter("updated_at", Between, []any{start, end}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( updated_at BETWEEN $1 AND $2 )", qp.Query)
	assert.Equal(t, start, qp.Params[0])
	assert.Equal(t, end, qp.Params[1])
}

func TestQueryBuild_Between_MySQL_UsesQuestionMark(t *testing.T) {
	fs := NewFilters().Add(NewFilter("created_at", Between, "2024-01-01T00:00:00Z|2024-12-31T23:59:59Z"))
	qp := NewQueryBuildWithDialect(base, fs, my)
	assert.Equal(t, base+" AND ( created_at BETWEEN ? AND ? )", qp.Query)
}

func TestQueryBuild_Between_SQLServer_UsesAtP(t *testing.T) {
	fs := NewFilters().Add(NewFilter("created_at", Between, "2024-01-01T00:00:00Z|2024-12-31T23:59:59Z"))
	qp := NewQueryBuildWithDialect(base, fs, mssql)
	assert.Equal(t, base+" AND ( created_at BETWEEN @p1 AND @p2 )", qp.Query)
}

func TestQueryBuild_Between_Oracle_UsesColonN(t *testing.T) {
	fs := NewFilters().Add(NewFilter("created_at", Between, "2024-01-01T00:00:00Z|2024-12-31T23:59:59Z"))
	qp := NewQueryBuildWithDialect(base, fs, ora)
	assert.Equal(t, base+" AND ( created_at BETWEEN :1 AND :2 )", qp.Query)
}

// Null Check Operators

func TestQueryBuild_IsNull_ExplicitCondition(t *testing.T) {
	fs := NewFilters().Add(NewFilter("deleted_at", IsNull, nil))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( deleted_at IS NULL )", qp.Query)
	assert.Empty(t, qp.Params)
}

func TestQueryBuild_IsNotNull_ExplicitCondition(t *testing.T) {
	fs := NewFilters().Add(NewFilter("deleted_at", IsNotNull, nil))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( deleted_at IS NOT NULL )", qp.Query)
	assert.Empty(t, qp.Params)
}

func TestQueryBuild_ImplicitIsNull_WhenValueNil(t *testing.T) {
	fs := NewFilters().Add(NewFilter("archived_at", Eq, nil))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( archived_at IS NULL )", qp.Query)
	assert.Empty(t, qp.Params)
}

// Logical Operators

func TestQueryBuild_MultipleFilters_WithAND(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("age", GreaterOrEqual, 18)).
		Add(AND()).
		Add(NewFilter("status", Eq, "active"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( age >= $1 AND status = $2 )", qp.Query)
	assert.Equal(t, []any{IntValue(18), StringValue("active")}, qp.Params)
}

func TestQueryBuild_MultipleFilters_WithOR(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("type", Eq, "A")).
		Add(OR()).
		Add(NewFilter("type", Eq, "B"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( type = $1 OR type = $2 )", qp.Query)
}

func TestQueryBuild_MixedLogicalOperators(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("a", Eq, 1)).
		Add(AND()).
		Add(NewFilter("b", Eq, 2)).
		Add(OR()).
		Add(NewFilter("c", Eq, 3))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( a = $1 AND b = $2 OR c = $3 )", qp.Query)
}

// Dialect Placeholders

func TestQueryBuild_MySQL_PlaceholderStyle(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("a", Eq, 1)).
		Add(AND()).
		Add(NewFilter("b", Eq, "x"))
	qp := NewQueryBuildWithDialect(base, fs, my)
	assert.Equal(t, base+" AND ( a = ? AND b = ? )", qp.Query)
}

func TestQueryBuild_SQLServer_PlaceholderStyle(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("a", Eq, 1)).
		Add(AND()).
		Add(NewFilter("b", Eq, "x"))
	qp := NewQueryBuildWithDialect(base, fs, mssql)
	assert.Equal(t, base+" AND ( a = @p1 AND b = @p2 )", qp.Query)
}

func TestQueryBuild_Oracle_PlaceholderStyle(t *testing.T) {
	fs := NewFilters().
		Add(NewFilter("a", Eq, 1)).
		Add(AND()).
		Add(NewFilter("b", Eq, "x"))
	qp := NewQueryBuildWithDialect(base, fs, ora)
	assert.Equal(t, base+" AND ( a = :1 AND b = :2 )", qp.Query)
}

// NewQueryBuild with explicit dialect (no global connection needed)

func TestNewQueryBuild_PostgresStyle_ExplicitDialect(t *testing.T) {
	// NewQueryBuild requires a global connection; use NewQueryBuildWithDialect for dialect control.
	fs := NewFilters().Add(NewFilter("status", Eq, "active"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( status = $1 )", qp.Query)
}

// Complex Scenario (matches user's example)

func TestQueryBuild_UserExampleScenario(t *testing.T) {
	// Base query uses 1=1 so filter params always start from $1.
	// Tenant filtering is typically applied as a literal in the base query
	// or as a pre-bound param whose value is prepended to Params by the caller.
	fs := NewFilters().
		Add(NewFilter("l.sku_marketplace", Eq, "MLB4957597486")).
		Add(AND()).
		Add(NewFilter("l.marketplace_id", Eq, []any{2})).
		Add(AND()).
		Add(NewFilter("l.buybox_highlight", NotEqual, []any{"NON_CATALOG"}))

	qp := NewQueryBuildWithDialect("SELECT * FROM listings l WHERE 1=1", fs, pg)

	assert.Equal(t,
		"SELECT * FROM listings l WHERE 1=1 AND ( l.sku_marketplace = $1 AND l.marketplace_id = $2 AND l.buybox_highlight != $3 )",
		qp.Query,
	)
	assert.Len(t, qp.Params, 3)
	assert.Equal(t, StringValue("MLB4957597486"), qp.Params[0])
	assert.Equal(t, IntValue(2), qp.Params[1])
	assert.Equal(t, StringValue("NON_CATALOG"), qp.Params[2])
}

// Security

func TestContainsNotAllowedValue_KnownKeywords(t *testing.T) {
	cases := []string{
		"DROP TABLE users",
		"DELETE FROM orders",
		"UPDATE accounts SET balance = 0",
		"INSERT INTO admin VALUES (1)",
		"ALTER TABLE t ADD COLUMN x",
		"UNION SELECT * FROM secrets",
		"EXEC xp_cmdshell('rm -rf /')",
		"SLEEP(5)",
		"BENCHMARK(1000000, MD5(1))",
	}
	for _, c := range cases {
		assert.True(t, ContainsNotAllowedValue(c), "expected blocked: %s", c)
	}
}

func TestContainsNotAllowedValue_SafeValues(t *testing.T) {
	safe := []string{
		"active", "pending", "MLB4957597486",
		"SELECTED", "PROCESSING",
		"EXECUTOR", // contains "EXEC" but not as a whole word
		"DELETED",  // contains "DELETE" but not as a whole word
		"INSERTED", // contains "INSERT" but not as a whole word
		"UPDATED",  // contains "UPDATE" but not as a whole word
		"GRANTED",  // contains "GRANT" but not as a whole word
	}
	for _, s := range safe {
		assert.False(t, ContainsNotAllowedValue(s), "expected allowed: %s", s)
	}
}

// Internal Helpers

func TestQueryBuild_InvalidCondition_FilterSkipped(t *testing.T) {
	// A filter with an unknown operator must be silently skipped; base query is returned as-is.
	fs := NewFilters().Add(NewFilter("field", "UNKNOWN_OP", "value"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base, qp.Query)
	assert.Empty(t, qp.Params)
}

func TestQueryBuild_InvalidLogicalOperator_FilterSkipped(t *testing.T) {
	// An invalid logical operator must be silently skipped; surrounding conditions still build.
	fs := NewFilters().
		Add(NewFilter("a", Eq, 1)).
		Add(&Filter{LogicalOperator: "XOR"}).
		Add(NewFilter("b", Eq, 2))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	// "XOR" filter is dropped; "a" and "b" remain, joined with implicit AND from criteria builder.
	assert.Contains(t, qp.Query, "a = $1")
	assert.Contains(t, qp.Query, "b = $2")
}

func TestQueryBuild_InvalidBetweenSlice_FilterSkipped(t *testing.T) {
	// BETWEEN with a slice that is not []time.Time must be silently skipped.
	fs := NewFilters().Add(NewFilter("created_at", Between, []any{"not-a-time", "also-not"}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base, qp.Query)
	assert.Empty(t, qp.Params)
}

func TestResolveValueType_TypeMapping(t *testing.T) {
	assert.Equal(t, StringValue("hello"), resolveValueType("hello"))
	assert.Equal(t, IntValue(42), resolveValueType(42))
	assert.Equal(t, IntValue(42), resolveValueType(int32(42)))
	assert.Equal(t, IntValue(42), resolveValueType(int64(42)))
	assert.Equal(t, FloatValue(3.14), resolveValueType(3.14))
	// float32 → float64 conversion introduces precision artifacts; check type only.
	result := resolveValueType(float32(3.14))
	_, ok := result.(FloatValue)
	assert.True(t, ok, "float32 should resolve to FloatValue")
}

func TestResolveValueType_DateString_ReturnsTimes(t *testing.T) {
	result := resolveValueType("2024-01-01T00:00:00Z|2024-12-31T23:59:59Z")
	times, ok := result.([]time.Time)
	assert.True(t, ok)
	assert.Len(t, times, 2)
}

func TestParseBetweenDates_Valid(t *testing.T) {
	start, _ := time.Parse(timeLayout, "2023-01-01T00:00:00Z")
	end, _ := time.Parse(timeLayout, "2023-12-31T23:59:59Z")
	dates, err := parseBetweenDates("2023-01-01T00:00:00Z|2023-12-31T23:59:59Z")
	assert.NoError(t, err)
	assert.Equal(t, []time.Time{start, end}, dates)
}

func TestParseBetweenDates_InvalidFormat(t *testing.T) {
	_, err := parseBetweenDates("2023-01-01T00:00:00Z")
	assert.Error(t, err)
}

func TestParseBetweenDates_InvalidDates(t *testing.T) {
	_, err := parseBetweenDates("not-a-date|also-not-a-date")
	assert.Error(t, err)
}

func TestToAnySlice_Slice(t *testing.T) {
	result, ok := toAnySlice([]any{1, "a", true})
	assert.True(t, ok)
	assert.Equal(t, []any{1, "a", true}, result)
}

func TestToAnySlice_NonSlice(t *testing.T) {
	_, ok := toAnySlice("not a slice")
	assert.False(t, ok)
}

func TestToTimeSlice_Valid(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(24 * time.Hour)
	times, ok := toTimeSlice([]any{t1, t2})
	assert.True(t, ok)
	assert.Equal(t, []time.Time{t1, t2}, times)
}

func TestToTimeSlice_InvalidElement(t *testing.T) {
	_, ok := toTimeSlice([]any{time.Now(), "not a time"})
	assert.False(t, ok)
}

// Generic OR Expansion (non-IN operators with slice)

func TestQueryBuild_LessOrEqual_SliceExpandsWithOR(t *testing.T) {
	fs := NewFilters().Add(NewFilter("score", LessOrEqual, []any{10, 20}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base+" AND ( (score <= $1 OR score <= $2) )", qp.Query)
	assert.Len(t, qp.Params, 2)
}

// buildSlicePredicate — empty slice is skipped

func TestQueryBuild_EmptySlice_FilterSkipped(t *testing.T) {
	fs := NewFilters().Add(NewFilter("id", In, []any{}))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	assert.Equal(t, base, qp.Query)
	assert.Empty(t, qp.Params)
}

// buildScalarPredicate — []time.Time value with a non-BETWEEN condition is rejected

func TestBuildScalarPredicate_TimeSlice_NonBetween_FilterSkipped(t *testing.T) {
	// A date-range string resolves to []time.Time; any condition other than BETWEEN must be rejected.
	fs := NewFilters().Add(NewFilter("created_at", Eq, "2024-01-01T00:00:00Z|2024-12-31T23:59:59Z"))
	qp := NewQueryBuildWithDialect(base, fs, pg)
	// The filter is skipped — base query is returned unchanged.
	assert.Equal(t, base, qp.Query)
	assert.Empty(t, qp.Params)
}

// scalarConditionPredicate — default branch (operator not handled by switch)

func TestScalarConditionPredicate_UnknownCondition_DefaultPath(t *testing.T) {
	// Call the unexported function directly from within the package.
	_, ok := scalarConditionPredicate("field", "UNSUPPORTED_OP", "value")
	assert.False(t, ok)
}

// resolveValueType — default branch (type not explicitly handled → StringValue fallback)

func TestResolveValueType_UnknownType_StringifiesValue(t *testing.T) {
	result := resolveValueType(true) // bool is not in the explicit switch cases
	assert.Equal(t, StringValue("true"), result)
}

// ---------------------------------------------------------------------------
// activeDialect / NewQueryBuild — auto-resolution from global connection
// ---------------------------------------------------------------------------

// filterTestDialect is a distinct dialect for injection tests.
// Uses @pN placeholders (SQL Server style) and LIKE — easily distinguishable
// from the PostgreSQL fallback ($N / ILIKE).
type filterTestDialect struct{}

func (filterTestDialect) Param(i int) string         { return fmt.Sprintf("@p%d", i) }
func (filterTestDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (filterTestDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }
func (filterTestDialect) BuildPagination(q, _ string, _, _ uint16) string {
	return q
}
func (filterTestDialect) BuildCount(q string) string {
	return "SELECT COUNT(*) FROM (" + q + ") AS t"
}

// filterTestDriver wraps a pre-built dialect for use with connection.NewRaw.
type filterTestDriver struct{ dialect sqln_driver.Dialect }

func (d filterTestDriver) Name() connection.DriverName               { return "filter-test-driver" }
func (d filterTestDriver) Open(_ connection.Config) (*sql.DB, error) { return nil, nil }
func (d filterTestDriver) ParseError(err error) error                { return err }
func (d filterTestDriver) Dialect() sqln_driver.Dialect              { return d.dialect }

// noopSQLDriver is the minimal database/sql driver needed to open a *sql.DB.
type noopSQLDriver struct{}
type noopConn struct{}
type noopStmt struct{}
type noopRows struct{}
type noopTx struct{}

func (noopSQLDriver) Open(_ string) (stdsqldriver.Conn, error) { return &noopConn{}, nil }
func (*noopConn) Prepare(_ string) (stdsqldriver.Stmt, error)  { return &noopStmt{}, nil }
func (*noopConn) Close() error                                 { return nil }
func (*noopConn) Begin() (stdsqldriver.Tx, error)              { return &noopTx{}, nil }
func (*noopConn) Ping(_ context.Context) error                 { return nil }
func (*noopStmt) Close() error                                 { return nil }
func (*noopStmt) NumInput() int                                { return -1 }
func (*noopStmt) Exec(_ []stdsqldriver.Value) (stdsqldriver.Result, error) {
	return stdsqldriver.RowsAffected(0), nil
}
func (*noopStmt) Query(_ []stdsqldriver.Value) (stdsqldriver.Rows, error) { return &noopRows{}, nil }
func (*noopRows) Columns() []string                                       { return nil }
func (*noopRows) Close() error                                            { return nil }
func (*noopRows) Next(_ []stdsqldriver.Value) error                       { return io.EOF }
func (*noopTx) Commit() error                                             { return nil }
func (*noopTx) Rollback() error                                           { return nil }

var registerNoopOnce sync.Once

func openNoopDB() *sql.DB {
	registerNoopOnce.Do(func() {
		sql.Register("noop-filter-test", noopSQLDriver{})
	})
	db, err := sql.Open("noop-filter-test", "")
	if err != nil {
		panic(err)
	}
	return db
}

// setFilterTestGlobalDialect injects a global connection that returns the given
// dialect, without hitting a real database.
func setFilterTestGlobalDialect(d sqln_driver.Dialect) {
	conn := connection.NewRaw(openNoopDB(), filterTestDriver{dialect: d})
	connection.SetGlobal(conn)
}

// TestActiveDialect_PanicsWhenNoGlobalConnection is in active_dialect_test.go.

// TestActiveDialect_UsesGlobalConnectionDialect verifies that after a global
// connection is established, activeDialect() returns its dialect, not the fallback.
func TestActiveDialect_UsesGlobalConnectionDialect(t *testing.T) {
	connection.ResetGlobalForTest()
	defer connection.ResetGlobalForTest()

	setFilterTestGlobalDialect(filterTestDialect{})

	d := activeDialect()
	// filterTestDialect uses @pN — completely different from the PG fallback.
	assert.Equal(t, "@p1", d.Param(1))
	assert.Equal(t, "@p3", d.Param(3))
	assert.NotContains(t, d.Like("name", "@p1"), "ILIKE")
}

// TestNewQueryBuild_PanicsWhenNoGlobalConnection is in active_dialect_test.go.

// TestNewQueryBuild_UsesGlobalDialect_WhenConnectionIsSet verifies that
// NewQueryBuild uses the dialect from the active connection — not the fallback —
// when AddDatabase() (or equivalent) has been called.
func TestNewQueryBuild_UsesGlobalDialect_WhenConnectionIsSet(t *testing.T) {
	connection.ResetGlobalForTest()
	defer connection.ResetGlobalForTest()

	setFilterTestGlobalDialect(filterTestDialect{})

	fs := NewFilters().Add(NewFilter("name", Eq, "Emilia"))
	qp := NewQueryBuild(base, fs)

	// filterTestDialect uses @pN placeholders, not PostgreSQL $N.
	assert.Equal(t, base+" AND ( name = @p1 )", qp.Query)
}

// TestNewQueryBuild_ContainsDialect_UsesGlobalDialectForLike verifies that
// the Contains operator (ILIKE vs LIKE) is also driven by the global dialect.
func TestNewQueryBuild_ContainsDialect_UsesGlobalDialectForLike(t *testing.T) {
	connection.ResetGlobalForTest()
	defer connection.ResetGlobalForTest()

	setFilterTestGlobalDialect(filterTestDialect{})

	fs := NewFilters().Add(NewFilter("name", Contains, "Emilia"))
	qp := NewQueryBuild(base, fs)

	// filterTestDialect.Like produces LIKE, not ILIKE.
	assert.Contains(t, qp.Query, "LIKE")
	assert.NotContains(t, qp.Query, "ILIKE")
}

// TestNewQueryBuildWithDialect_ExplicitDialectTakesPrecedence verifies that
// when a dialect is passed explicitly, it overrides the global connection dialect.
func TestNewQueryBuildWithDialect_ExplicitDialectTakesPrecedence(t *testing.T) {
	connection.ResetGlobalForTest()
	defer connection.ResetGlobalForTest()

	// Global uses filterTestDialect (@pN / LIKE).
	setFilterTestGlobalDialect(filterTestDialect{})

	// But we pass pgDialect explicitly — it must win.
	fs := NewFilters().Add(NewFilter("status", Eq, "ok"))
	qp := NewQueryBuildWithDialect(base, fs, pg)

	assert.Equal(t, base+" AND ( status = $1 )", qp.Query)
}

// TestNewQueryBuildWithDialect_NilDialect_FallsBackToGlobal verifies that
// passing nil as the dialect falls back to the global (or PG if no connection).
func TestNewQueryBuildWithDialect_NilDialect_FallsBackToGlobal(t *testing.T) {
	connection.ResetGlobalForTest()
	defer connection.ResetGlobalForTest()

	setFilterTestGlobalDialect(filterTestDialect{})

	fs := NewFilters().Add(NewFilter("id", Eq, 42))
	qp := NewQueryBuildWithDialect(base, fs, nil)

	// nil dialect falls back to the global → filterTestDialect → @pN.
	assert.Equal(t, base+" AND ( id = @p1 )", qp.Query)
}

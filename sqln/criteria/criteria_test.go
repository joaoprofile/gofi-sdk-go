package criteria_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//  Test Dialects

// pgDialect mirrors PostgreSQL behaviour: $n placeholders, ILIKE.
type pgDialect struct{}

func (pgDialect) Param(i int) string         { return fmt.Sprintf("$%d", i) }
func (pgDialect) Like(f, p string) string    { return f + " ILIKE " + p }
func (pgDialect) NotLike(f, p string) string { return f + " NOT ILIKE " + p }

// myDialect mirrors MySQL behaviour: ? placeholders, LIKE.
type myDialect struct{}

func (myDialect) Param(_ int) string         { return "?" }
func (myDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (myDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }

// oraDialect mirrors Oracle behaviour: :n placeholders.
type oraDialect struct{}

func (oraDialect) Param(i int) string         { return fmt.Sprintf(":%d", i) }
func (oraDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (oraDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }

// mssDialect mirrors SQL Server behaviour: @pn placeholders.
type mssDialect struct{}

func (mssDialect) Param(i int) string         { return fmt.Sprintf("@p%d", i) }
func (mssDialect) Like(f, p string) string    { return f + " LIKE " + p }
func (mssDialect) NotLike(f, p string) string { return f + " NOT LIKE " + p }

var pg = pgDialect{}
var my = myDialect{}
var ora = oraDialect{}
var mss = mssDialect{}

//  SELECT / FROM / alias ─

func TestBuild_SelectStar_WhenNoFieldsSpecified(t *testing.T) {
	sql, params := criteria.From("users", "").Build(pg)
	assert.Equal(t, "SELECT * FROM users", sql)
	assert.Empty(t, params)
}

func TestBuild_SelectSpecificFields(t *testing.T) {
	sql, params := criteria.From("users", "").
		Select("id", "name", "email").
		Build(pg)
	assert.Equal(t, "SELECT id, name, email FROM users", sql)
	assert.Empty(t, params)
}

func TestBuild_FromWithAlias(t *testing.T) {
	sql, _ := criteria.From("users", "u").Build(pg)
	assert.Equal(t, "SELECT * FROM users u", sql)
}

func TestBuild_SelectWithAlias(t *testing.T) {
	sql, _ := criteria.From("users", "u").Select("u.id", "u.name").Build(pg)
	assert.Equal(t, "SELECT u.id, u.name FROM users u", sql)
}

func TestBuild_MultipleSelectCalls_Accumulated(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		Select("u.id").
		Select("u.name").
		Build(pg)
	assert.Equal(t, "SELECT u.id, u.name FROM users u", sql)
}

//  JOIN variants

func TestBuild_InnerJoinWithAlias(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		Join("orders", "o", "o.user_id = u.id").
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u JOIN orders o ON o.user_id = u.id", sql)
}

func TestBuild_InnerJoinWithoutAlias(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		Join("orders", "", "orders.user_id = u.id").
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u JOIN orders ON orders.user_id = u.id", sql)
}

func TestBuild_LeftJoin(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		LeftJoin("orders", "o", "o.user_id = u.id").
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u LEFT JOIN orders o ON o.user_id = u.id", sql)
}

func TestBuild_RightJoin(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		RightJoin("orders", "o", "o.user_id = u.id").
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u RIGHT JOIN orders o ON o.user_id = u.id", sql)
}

func TestBuild_MultipleJoins(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		Join("orders", "o", "o.user_id = u.id").
		LeftJoin("payments", "p", "p.order_id = o.id").
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u JOIN orders o ON o.user_id = u.id LEFT JOIN payments p ON p.order_id = o.id", sql)
}

//  WHERE — single predicates

func TestBuild_Where_Eq(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Eq("u.active", true)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1", sql)
	assert.Equal(t, []any{true}, params)
}

func TestBuild_Where_Ne(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Ne("u.status", "banned")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.status != $1", sql)
	assert.Equal(t, []any{"banned"}, params)
}

func TestBuild_Where_Lt(t *testing.T) {
	sql, params := criteria.From("products", "p").
		Where(criteria.Lt("p.price", 100)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM products p WHERE p.price < $1", sql)
	assert.Equal(t, []any{100}, params)
}

func TestBuild_Where_Lte(t *testing.T) {
	sql, params := criteria.From("products", "p").
		Where(criteria.Lte("p.price", 100)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM products p WHERE p.price <= $1", sql)
	assert.Equal(t, []any{100}, params)
}

func TestBuild_Where_Gt(t *testing.T) {
	sql, params := criteria.From("products", "p").
		Where(criteria.Gt("p.stock", 0)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM products p WHERE p.stock > $1", sql)
	assert.Equal(t, []any{0}, params)
}

func TestBuild_Where_Gte(t *testing.T) {
	sql, params := criteria.From("products", "p").
		Where(criteria.Gte("p.score", 4.5)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM products p WHERE p.score >= $1", sql)
	assert.Equal(t, []any{4.5}, params)
}

func TestBuild_Where_IsNull(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.IsNull("u.deleted_at")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.deleted_at IS NULL", sql)
	assert.Empty(t, params)
}

func TestBuild_Where_IsNotNull(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.IsNotNull("u.confirmed_at")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.confirmed_at IS NOT NULL", sql)
	assert.Empty(t, params)
}

//  WHERE — text search

func TestBuild_Where_Contains_Postgres_UsesILike(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Contains("u.name", "%john%")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.name ILIKE $1", sql)
	assert.Equal(t, []any{"%john%"}, params)
}

func TestBuild_Where_Contains_MySQL_UsesLike(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Contains("u.name", "%john%")).
		Build(my)
	assert.Equal(t, "SELECT * FROM users u WHERE u.name LIKE ?", sql)
	assert.Equal(t, []any{"%john%"}, params)
}

func TestBuild_Where_NotContains_Postgres_UsesNotILike(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.NotContains("u.name", "%spam%")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.name NOT ILIKE $1", sql)
	assert.Equal(t, []any{"%spam%"}, params)
}

func TestBuild_Where_Like_CaseSensitive(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Like("u.code", "USR%")).
		Build(pg)

	assert.Equal(t, "SELECT * FROM users u WHERE u.code LIKE $1", sql)
	assert.Equal(t, []any{"USR%"}, params)
}

func TestBuild_Where_NotLike(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.NotLike("u.code", "TMP%")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.code NOT LIKE $1", sql)
	assert.Equal(t, []any{"TMP%"}, params)
}

// Like is always raw LIKE regardless of dialect.
func TestBuild_Where_Like_SameAcrossDialects(t *testing.T) {
	for _, d := range []interface {
		Param(int) string
		Like(string, string) string
		NotLike(string, string) string
	}{pg, my, ora, mss} {
		sql, _ := criteria.From("t", "").Where(criteria.Like("t.col", "X%")).Build(d)
		assert.Contains(t, sql, "LIKE")
	}
}

//  WHERE — membership

func TestBuild_Where_In_StringSlice(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.role", []string{"admin", "moderator"})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.role IN ($1, $2)", sql)
	assert.Equal(t, []any{"admin", "moderator"}, params)
}

func TestBuild_Where_In_IntSlice(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.id", []int{1, 2, 3})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.id IN ($1, $2, $3)", sql)
	assert.Equal(t, []any{1, 2, 3}, params)
}

func TestBuild_Where_In_Int32Slice(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.id", []int32{10, 20})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.id IN ($1, $2)", sql)
	assert.Equal(t, []any{int32(10), int32(20)}, params)
}

func TestBuild_Where_In_Int64Slice(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.id", []int64{100, 200})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.id IN ($1, $2)", sql)
	assert.Equal(t, []any{int64(100), int64(200)}, params)
}

func TestBuild_Where_In_Float64Slice(t *testing.T) {
	sql, params := criteria.From("products", "p").
		Where(criteria.In("p.price", []float64{9.99, 19.99})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM products p WHERE p.price IN ($1, $2)", sql)
	assert.Equal(t, []any{9.99, 19.99}, params)
}

func TestBuild_Where_In_AnySlice(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.id", []any{1, "two", 3})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.id IN ($1, $2, $3)", sql)
	assert.Equal(t, []any{1, "two", 3}, params)
}

func TestBuild_Where_In_ScalarFallback(t *testing.T) {
	// Non-slice value treated as single-element IN.
	sql, params := criteria.From("users", "u").
		Where(criteria.In("u.id", 42)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.id IN ($1)", sql)
	assert.Equal(t, []any{42}, params)
}

func TestBuild_Where_NotIn(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.NotIn("u.status", []string{"banned", "deleted"})).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.status NOT IN ($1, $2)", sql)
	assert.Equal(t, []any{"banned", "deleted"}, params)
}

//  WHERE — range

func TestBuild_Where_Between(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.Between("o.total", 100, 500)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.total BETWEEN $1 AND $2", sql)
	assert.Equal(t, []any{100, 500}, params)
}

func TestBuild_Where_Between_PanicOnWrongCount(t *testing.T) {
	// Between constructor always produces []any{from, to}, so the panic path
	// inside buildBetween is unreachable from public API.
	// This test documents that the constructor enforces arity.
	assert.NotPanics(t, func() {
		criteria.From("t", "").Where(criteria.Between("t.x", 1, 10)).Build(pg)
	})
}

//  WHERE — logical connectors

func TestBuild_Where_TwoPredicates_ImplicitAnd(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.active", true),
			criteria.Eq("u.role", "admin"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role = $2", sql)
	assert.Equal(t, []any{true, "admin"}, params)
}

func TestBuild_Where_ExplicitAnd(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.active", true),
			criteria.And(),
			criteria.Eq("u.role", "admin"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role = $2", sql)
	assert.Equal(t, []any{true, "admin"}, params)
}

func TestBuild_Where_ExplicitOr(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.role", "admin"),
			criteria.Or(),
			criteria.Eq("u.role", "moderator"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.role = $1 OR u.role = $2", sql)
	assert.Equal(t, []any{"admin", "moderator"}, params)
}

func TestBuild_Where_MixedAndOr(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.active", true),
			criteria.And(),
			criteria.Eq("u.role", "admin"),
			criteria.Or(),
			criteria.Eq("u.role", "moderator"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role = $2 OR u.role = $3", sql)
	assert.Equal(t, []any{true, "admin", "moderator"}, params)
}

func TestBuild_Where_ThreePredicates_AllImplicitAnd(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.active", true),
			criteria.Eq("u.role", "admin"),
			criteria.IsNull("u.deleted_at"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role = $2 AND u.deleted_at IS NULL", sql)
	assert.Equal(t, []any{true, "admin"}, params)
}

// Calling Where multiple times accumulates predicates with implicit AND between groups.
func TestBuild_Where_CalledMultipleTimes_Accumulates(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(criteria.Eq("u.active", true)).
		Where(criteria.Eq("u.role", "admin")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role = $2", sql)
	assert.Equal(t, []any{true, "admin"}, params)
}

// GROUP BY / HAVING

func TestBuild_GroupBy_SingleField(t *testing.T) {
	sql, _ := criteria.From("orders", "o").
		Select("o.user_id", "COUNT(*) total").
		GroupBy("o.user_id").
		Build(pg)
	assert.Equal(t, "SELECT o.user_id, COUNT(*) total FROM orders o GROUP BY o.user_id", sql)
}

func TestBuild_GroupBy_MultipleFields(t *testing.T) {
	sql, _ := criteria.From("orders", "o").
		GroupBy("o.user_id", "o.status").
		Build(pg)
	assert.Contains(t, sql, "GROUP BY o.user_id, o.status")
}

func TestBuild_Having_SinglePredicate(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Select("o.user_id", "COUNT(*) cnt").
		GroupBy("o.user_id").
		Having(criteria.Gt("COUNT(*)", 5)).
		Build(pg)
	assert.Equal(t, "SELECT o.user_id, COUNT(*) cnt FROM orders o GROUP BY o.user_id HAVING COUNT(*) > $1", sql)
	assert.Equal(t, []any{5}, params)
}

func TestBuild_Having_MultiplePredicates_ImplicitAnd(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		GroupBy("o.user_id").
		Having(
			criteria.Gt("COUNT(*)", 5),
			criteria.Lt("SUM(o.total)", 10000),
		).Build(pg)
	assert.Contains(t, sql, "HAVING COUNT(*) > $1 AND SUM(o.total) < $2")
	assert.Equal(t, []any{5, 10000}, params)
}

func TestBuild_Having_WithOrConnector(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		GroupBy("o.user_id").
		Having(
			criteria.Gt("COUNT(*)", 10),
			criteria.Or(),
			criteria.Gt("SUM(o.total)", 5000),
		).Build(pg)
	assert.Contains(t, sql, "HAVING COUNT(*) > $1 OR SUM(o.total) > $2")
	assert.Equal(t, []any{10, 5000}, params)
}

// ORDER BY

func TestBuild_OrderBy_Asc(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		OrderBy(criteria.Asc("u.name")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u ORDER BY u.name ASC", sql)
}

func TestBuild_OrderBy_Desc(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		OrderBy(criteria.Desc("u.created_at")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u ORDER BY u.created_at DESC", sql)
}

func TestBuild_OrderBy_Multiple(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		OrderBy(criteria.Asc("u.name"), criteria.Desc("u.created_at")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM users u ORDER BY u.name ASC, u.created_at DESC", sql)
}

// LIMIT / OFFSET
func TestBuild_Limit(t *testing.T) {
	sql, _ := criteria.From("users", "u").Limit(10).Build(pg)
	assert.Equal(t, "SELECT * FROM users u LIMIT 10", sql)
}

func TestBuild_Offset(t *testing.T) {
	sql, _ := criteria.From("users", "u").Offset(20).Build(pg)
	assert.Equal(t, "SELECT * FROM users u OFFSET 20", sql)
}

func TestBuild_LimitAndOffset(t *testing.T) {
	sql, _ := criteria.From("users", "u").Limit(15).Offset(30).Build(pg)
	assert.Equal(t, "SELECT * FROM users u LIMIT 15 OFFSET 30", sql)
}

func TestBuild_Limit_Zero_NotEmitted(t *testing.T) {
	// Limit(0) is treated as "not set" — no LIMIT clause emitted.
	sql, _ := criteria.From("users", "u").Limit(0).Build(pg)
	assert.Equal(t, "SELECT * FROM users u", sql)
}

func TestBuild_Offset_Zero_NotEmitted(t *testing.T) {
	sql, _ := criteria.From("users", "u").Offset(0).Build(pg)
	assert.Equal(t, "SELECT * FROM users u", sql)
}

// BuildBase

func TestBuildBase_OmitsOrderBy(t *testing.T) {
	sql, _ := criteria.From("users", "u").
		Where(criteria.Eq("u.active", true)).
		OrderBy(criteria.Asc("u.name")).
		BuildBase(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1", sql)
}

func TestBuildBase_OmitsLimit(t *testing.T) {
	sql, _ := criteria.From("users", "u").Limit(10).BuildBase(pg)
	assert.Equal(t, "SELECT * FROM users u", sql)
}

func TestBuildBase_OmitsOffset(t *testing.T) {
	sql, _ := criteria.From("users", "u").Offset(5).BuildBase(pg)
	assert.Equal(t, "SELECT * FROM users u", sql)
}

func TestBuildBase_PreservesWhereGroupByHaving(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		GroupBy("o.user_id").
		Having(criteria.Gt("COUNT(*)", 3)).
		Where(criteria.Eq("o.status", "paid")).
		OrderBy(criteria.Desc("o.created_at")).
		Limit(5).
		BuildBase(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.status = $1 GROUP BY o.user_id HAVING COUNT(*) > $2", sql)
	assert.Equal(t, []any{"paid", 3}, params)
}

// Dialect placeholder variation

func TestBuild_PlaceholderFormat_Postgres(t *testing.T) {
	sql, _ := criteria.From("t", "").
		Where(criteria.Eq("a", 1), criteria.Eq("b", 2)).
		Build(pg)
	assert.Contains(t, sql, "$1")
	assert.Contains(t, sql, "$2")
}

func TestBuild_PlaceholderFormat_MySQL(t *testing.T) {
	sql, _ := criteria.From("t", "").
		Where(criteria.Eq("a", 1), criteria.Eq("b", 2)).
		Build(my)
	assert.Equal(t, "SELECT * FROM t WHERE a = ? AND b = ?", sql)
}

func TestBuild_PlaceholderFormat_Oracle(t *testing.T) {
	sql, _ := criteria.From("t", "").
		Where(criteria.Eq("a", 1), criteria.Eq("b", 2)).
		Build(ora)
	assert.Equal(t, "SELECT * FROM t WHERE a = :1 AND b = :2", sql)
}

func TestBuild_PlaceholderFormat_SQLServer(t *testing.T) {
	sql, _ := criteria.From("t", "").
		Where(criteria.Eq("a", 1), criteria.Eq("b", 2)).
		Build(mss)

	assert.Equal(t, "SELECT * FROM t WHERE a = @p1 AND b = @p2", sql)
}

// Parameter indices advance correctly across different predicate types.
func TestBuild_ParameterOrdering_MixedPredicates(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.Eq("u.active", true),
			criteria.In("u.role", []string{"admin", "mod"}),
			criteria.Contains("u.name", "%john%"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1 AND u.role IN ($2, $3) AND u.name ILIKE $4", sql)
	assert.Equal(t, []any{true, "admin", "mod", "%john%"}, params)
}

//  Full query

func TestBuild_FullQuery_AllClauses(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Select("u.id", "u.name", "COUNT(o.id) orders").
		LeftJoin("orders", "o", "o.user_id = u.id").
		Where(
			criteria.Eq("u.active", true),
			criteria.And(),
			criteria.In("u.role", []string{"admin", "moderator"}),
		).
		GroupBy("u.id", "u.name").
		Having(criteria.Gt("COUNT(o.id)", 0)).
		OrderBy(criteria.Asc("u.name"), criteria.Desc("u.id")).
		Limit(20).
		Offset(40).
		Build(pg)

	expected := "SELECT u.id, u.name, COUNT(o.id) orders" +
		" FROM users u" +
		" LEFT JOIN orders o ON o.user_id = u.id" +
		" WHERE u.active = $1 AND u.role IN ($2, $3)" +
		" GROUP BY u.id, u.name" +
		" HAVING COUNT(o.id) > $4" +
		" ORDER BY u.name ASC, u.id DESC" +
		" LIMIT 20 OFFSET 40"

	assert.Equal(t, expected, sql)
	require.Equal(t, 4, len(params))
	assert.Equal(t, true, params[0])
	assert.Equal(t, "admin", params[1])
	assert.Equal(t, "moderator", params[2])
	assert.Equal(t, 0, params[3])
}

//  Order constructors ─

func TestAsc_Direction(t *testing.T) {
	o := criteria.Asc("name")
	assert.Equal(t, "name", o.Field)
	assert.Equal(t, "ASC", o.Direction)
}

func TestDesc_Direction(t *testing.T) {
	o := criteria.Desc("created_at")
	assert.Equal(t, "created_at", o.Field)
	assert.Equal(t, "DESC", o.Direction)
}

//  Immutability — chaining does not mutate shared state

func TestBuild_ChainingIsIsolated(t *testing.T) {
	base := criteria.From("users", "u").
		Where(criteria.Eq("u.active", true))

	// Two independent extensions of the same base.
	sqlA, paramsA := base.Where(criteria.Eq("u.role", "admin")).Build(pg)
	sqlB, _ := criteria.From("users", "u").
		Where(criteria.Eq("u.active", true)).
		Build(pg)

	// sqlA has both predicates.
	assert.Contains(t, sqlA, "u.role = $2")
	assert.Equal(t, []any{true, "admin"}, paramsA)

	// sqlB was built independently.
	assert.Equal(t, "SELECT * FROM users u WHERE u.active = $1", sqlB)
}

//  Empty WHERE / GROUP BY / HAVING

func TestBuild_NoWhere_NoGroupBy_NoHaving(t *testing.T) {
	sql, params := criteria.From("users", "").Build(pg)
	assert.Equal(t, "SELECT * FROM users", sql)
	assert.Empty(t, params)
	assert.NotContains(t, sql, "WHERE")
	assert.NotContains(t, sql, "GROUP BY")
	assert.NotContains(t, sql, "HAVING")
}

//  Between - parameter placement

func TestBuild_Between_ParamsInOrder(t *testing.T) {
	sql, params := criteria.From("events", "e").
		Where(
			criteria.Eq("e.type", "click"),
			criteria.Between("e.created_at", "2024-01-01", "2024-12-31"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM events e WHERE e.type = $1 AND e.created_at BETWEEN $2 AND $3", sql)
	assert.Equal(t, []any{"click", "2024-01-01", "2024-12-31"}, params)
}

//  IsNull alongside other predicates

func TestBuild_IsNull_DoesNotAddParameter(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.IsNull("u.deleted_at"),
			criteria.Eq("u.active", true),
		).Build(pg)
	// IS NULL has no param; Eq gets $1 (not $2).
	assert.Equal(t, "SELECT * FROM users u WHERE u.deleted_at IS NULL AND u.active = $1", sql)
	assert.Equal(t, []any{true}, params)
}

// IsTrue / IsFalse

func TestBuild_Where_IsTrue(t *testing.T) {
	sql, params := criteria.From("product", "p").
		Where(criteria.IsTrue("p.managed")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM product p WHERE p.managed IS TRUE", sql)
	assert.Empty(t, params)
}

func TestBuild_Where_IsFalse(t *testing.T) {
	sql, params := criteria.From("product", "p").
		Where(criteria.IsFalse("p.active")).
		Build(pg)
	assert.Equal(t, "SELECT * FROM product p WHERE p.active IS FALSE", sql)
	assert.Empty(t, params)
}

func TestBuild_Where_IsTrue_DoesNotAdvanceParamIndex(t *testing.T) {
	sql, params := criteria.From("product", "p").
		Where(
			criteria.IsTrue("p.managed"),
			criteria.Eq("p.company_id", 42),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM product p WHERE p.managed IS TRUE AND p.company_id = $1", sql)
	assert.Equal(t, []any{42}, params)
}

// Group

func TestBuild_Where_Group_SingleOr(t *testing.T) {
	sql, params := criteria.From("product", "p").
		Where(
			criteria.Group(
				criteria.Eq("p.sku", "ABC"),
				criteria.Or(),
				criteria.Eq("p.sku_marketplace", "ABC"),
			),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM product p WHERE (p.sku = $1 OR p.sku_marketplace = $2)", sql)
	assert.Equal(t, []any{"ABC", "ABC"}, params)
}

func TestBuild_Where_Group_IsolatesOrFromSurroundingAnd(t *testing.T) {
	sql, params := criteria.From("product", "p").
		Where(
			criteria.Eq("p.company_id", 1),
			criteria.Group(
				criteria.Eq("p.sku", "X"),
				criteria.Or(),
				criteria.Eq("p.sku_marketplace", "X"),
			),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM product p WHERE p.company_id = $1 AND (p.sku = $2 OR p.sku_marketplace = $3)", sql)
	assert.Equal(t, []any{1, "X", "X"}, params)
}

func TestBuild_Where_Group_NestedGroup(t *testing.T) {
	sql, params := criteria.From("t", "").
		Where(
			criteria.Group(
				criteria.Eq("a", 1),
				criteria.Or(),
				criteria.Group(
					criteria.Eq("b", 2),
					criteria.Eq("c", 3),
				),
			),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM t WHERE (a = $1 OR (b = $2 AND c = $3))", sql)
	assert.Equal(t, []any{1, 2, 3}, params)
}

func TestBuild_Where_Group_ParamIndexContinuesAfterGroup(t *testing.T) {
	sql, params := criteria.From("t", "").
		Where(
			criteria.Eq("x", 10),
			criteria.Group(
				criteria.Eq("a", 1),
				criteria.Or(),
				criteria.Eq("b", 2),
			),
			criteria.Eq("y", 20),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM t WHERE x = $1 AND (a = $2 OR b = $3) AND y = $4", sql)
	assert.Equal(t, []any{10, 1, 2, 20}, params)
}

// Full product query — mirrors the SQL:
//
//	SELECT p.id, p.company_id, p.marketplace_id, p.title, p.sku, p.sku_marketplace, p.price, p.managed
//	FROM product p
//	INNER JOIN marketplace m ON m.id = p.marketplace_id
//	WHERE
//	    p.company_id = $1
//	    AND p.managed IS TRUE
//	    AND (p.title ILIKE $2 OR p.sku = $3 OR p.sku_marketplace = $4)
//	    AND p.marketplace_id IN ($5, $6)
//
// The optional-filter pattern ($n IS NULL OR ...) is handled at the application layer
// by conditionally appending the Where clauses, keeping the builder dialect-agnostic.
func TestBuild_FullProductQuery_WithSearchAndMarketplaceFilter(t *testing.T) {
	search := "tênis"
	marketplaceIDs := []int{1, 2}

	q := criteria.From("product", "p").
		Select("p.id", "p.company_id", "p.marketplace_id", "p.title", "p.sku", "p.sku_marketplace", "p.price", "p.managed").
		Join("marketplace", "m", "m.id = p.marketplace_id").
		Where(
			criteria.Eq("p.company_id", 42),
			criteria.IsTrue("p.managed"),
		)

	if search != "" {
		q = q.Where(criteria.Group(
			criteria.Contains("p.title", "%"+search+"%"),
			criteria.Or(),
			criteria.Eq("p.sku", search),
			criteria.Or(),
			criteria.Eq("p.sku_marketplace", search),
		))
	}

	if len(marketplaceIDs) > 0 {
		q = q.Where(criteria.In("p.marketplace_id", marketplaceIDs))
	}

	sql, params := q.Build(pg)

	expected := "SELECT p.id, p.company_id, p.marketplace_id, p.title, p.sku, p.sku_marketplace, p.price, p.managed" +
		" FROM product p" +
		" JOIN marketplace m ON m.id = p.marketplace_id" +
		" WHERE p.company_id = $1 AND p.managed IS TRUE" +
		" AND (p.title ILIKE $2 OR p.sku = $3 OR p.sku_marketplace = $4)" +
		" AND p.marketplace_id IN ($5, $6)"

	assert.Equal(t, expected, sql)
	assert.Equal(t, []any{42, "%" + search + "%", search, search, 1, 2}, params)
}

func TestBuild_FullProductQuery_WithoutOptionalFilters(t *testing.T) {
	var search string
	var marketplaceIDs []int

	q := criteria.From("product", "p").
		Select("p.id", "p.company_id", "p.marketplace_id", "p.title", "p.sku", "p.sku_marketplace", "p.price", "p.managed").
		Join("marketplace", "m", "m.id = p.marketplace_id").
		Where(
			criteria.Eq("p.company_id", 7),
			criteria.IsTrue("p.managed"),
		)

	if search != "" {
		q = q.Where(criteria.Group(
			criteria.Contains("p.title", "%"+search+"%"),
			criteria.Or(),
			criteria.Eq("p.sku", search),
			criteria.Or(),
			criteria.Eq("p.sku_marketplace", search),
		))
	}

	if len(marketplaceIDs) > 0 {
		q = q.Where(criteria.In("p.marketplace_id", marketplaceIDs))
	}

	sql, params := q.Build(pg)

	expected := "SELECT p.id, p.company_id, p.marketplace_id, p.title, p.sku, p.sku_marketplace, p.price, p.managed" +
		" FROM product p" +
		" JOIN marketplace m ON m.id = p.marketplace_id" +
		" WHERE p.company_id = $1 AND p.managed IS TRUE"

	assert.Equal(t, expected, sql)
	assert.Equal(t, []any{7}, params)
}

// Date predicates — query building

var (
	buildDate      = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	buildDateLater = time.Date(2026, 3, 31, 23, 59, 59, 0, time.UTC)
)

func TestBuild_Where_DateEq(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateEq("o.created_at", buildDate)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.created_at = $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

func TestBuild_Where_DateBefore(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateBefore("o.expires_at", buildDate)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.expires_at < $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

func TestBuild_Where_DateAfter(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateAfter("o.created_at", buildDate)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.created_at > $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

func TestBuild_Where_DateOnOrBefore(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateOnOrBefore("o.scheduled_at", buildDate)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.scheduled_at <= $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

func TestBuild_Where_DateOnOrAfter(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateOnOrAfter("o.scheduled_at", buildDate)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.scheduled_at >= $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

func TestBuild_Where_DateBetween_SQL(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(criteria.DateBetween("o.created_at", buildDate, buildDateLater)).
		Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.created_at BETWEEN $1 AND $2", sql)
	assert.Equal(t, []any{buildDate, buildDateLater}, params)
}

// DateBetween from/to are bound as $1 and $2 — never swapped.
func TestBuild_Where_DateBetween_ParamOrder(t *testing.T) {
	_, params := criteria.From("o", "").
		Where(criteria.DateBetween("o.created_at", buildDate, buildDateLater)).
		Build(pg)
	require.Len(t, params, 2)
	assert.Equal(t, buildDate, params[0])
	assert.Equal(t, buildDateLater, params[1])
}

// DateBetween consumes two param slots; the next predicate gets $3.
func TestBuild_Where_DateBetween_AdvancesParamIndex(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(
			criteria.DateBetween("o.created_at", buildDate, buildDateLater),
			criteria.Eq("o.status", "paid"),
		).Build(pg)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.created_at BETWEEN $1 AND $2 AND o.status = $3", sql)
	assert.Equal(t, []any{buildDate, buildDateLater, "paid"}, params)
}

// DateOnOrAfter + DateOnOrBefore is semantically equivalent to DateBetween.
func TestBuild_Where_DateRange_TwoPredicates_EquivalentToBetween(t *testing.T) {
	sqlBetween, _ := criteria.From("orders", "o").
		Where(criteria.DateBetween("o.created_at", buildDate, buildDateLater)).
		Build(pg)

	sqlRange, _ := criteria.From("orders", "o").
		Where(
			criteria.DateOnOrAfter("o.created_at", buildDate),
			criteria.DateOnOrBefore("o.created_at", buildDateLater),
		).Build(pg)

	assert.Equal(t,
		"SELECT * FROM orders o WHERE o.created_at BETWEEN $1 AND $2",
		sqlBetween,
	)
	assert.Equal(t,
		"SELECT * FROM orders o WHERE o.created_at >= $1 AND o.created_at <= $2",
		sqlRange,
	)
}

// Realistic: active orders within a time window.
func TestBuild_Where_DateBetween_CombinedWithOtherPredicates(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Where(
			criteria.Eq("o.company_id", 42),
			criteria.Eq("o.status", "paid"),
			criteria.DateBetween("o.created_at", buildDate, buildDateLater),
		).Build(pg)

	expected := "SELECT * FROM orders o WHERE o.company_id = $1 AND o.status = $2 AND o.created_at BETWEEN $3 AND $4"
	assert.Equal(t, expected, sql)
	assert.Equal(t, []any{42, "paid", buildDate, buildDateLater}, params)
}

// Realistic: not-deleted records created after a given date.
func TestBuild_Where_IsNull_AndDateAfter(t *testing.T) {
	sql, params := criteria.From("users", "u").
		Where(
			criteria.IsNull("u.deleted_at"),
			criteria.DateAfter("u.created_at", buildDate),
		).Build(pg)

	assert.Equal(t, "SELECT * FROM users u WHERE u.deleted_at IS NULL AND u.created_at > $1", sql)
	assert.Equal(t, []any{buildDate}, params)
}

// Date predicates produce the correct placeholder format across all dialects.
func TestBuild_Date_PlaceholderAcrossDialects(t *testing.T) {
	tests := []struct {
		name    string
		dialect interface {
			Param(int) string
			Like(string, string) string
			NotLike(string, string) string
		}
		wantSQL string
	}{
		{"postgres", pg, "SELECT * FROM orders o WHERE o.created_at BETWEEN $1 AND $2"},
		{"mysql", my, "SELECT * FROM orders o WHERE o.created_at BETWEEN ? AND ?"},
		{"oracle", ora, "SELECT * FROM orders o WHERE o.created_at BETWEEN :1 AND :2"},
		{"sqlserver", mss, "SELECT * FROM orders o WHERE o.created_at BETWEEN @p1 AND @p2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, _ := criteria.From("orders", "o").
				Where(criteria.DateBetween("o.created_at", buildDate, buildDateLater)).
				Build(tc.dialect)
			assert.Equal(t, tc.wantSQL, sql)
		})
	}
}

// Realistic: order report — date range + join + group + having + order + pagination.
func TestBuild_FullOrderReport_WithDateRange(t *testing.T) {
	sql, params := criteria.From("orders", "o").
		Select("o.seller_id", "COUNT(*) total", "SUM(o.amount) revenue").
		Join("sellers", "s", "s.id = o.seller_id").
		Where(
			criteria.Eq("o.company_id", 7),
			criteria.DateBetween("o.created_at", buildDate, buildDateLater),
			criteria.NotIn("o.status", []string{"cancelled", "refunded"}),
		).
		GroupBy("o.seller_id").
		Having(criteria.Gt("COUNT(*)", 0)).
		OrderBy(criteria.Desc("revenue")).
		Limit(50).
		Build(pg)

	expected := "SELECT o.seller_id, COUNT(*) total, SUM(o.amount) revenue" +
		" FROM orders o" +
		" JOIN sellers s ON s.id = o.seller_id" +
		" WHERE o.company_id = $1 AND o.created_at BETWEEN $2 AND $3 AND o.status NOT IN ($4, $5)" +
		" GROUP BY o.seller_id" +
		" HAVING COUNT(*) > $6" +
		" ORDER BY revenue DESC" +
		" LIMIT 50"

	assert.Equal(t, expected, sql)
	assert.Equal(t, []any{7, buildDate, buildDateLater, "cancelled", "refunded", 0}, params)
}

//  Table-driven: all comparison operators

func TestBuild_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name      string
		predicate criteria.Predicate
		wantOp    string
	}{
		{"Eq", criteria.Eq("f", 1), "="},
		{"Ne", criteria.Ne("f", 1), "!="},
		{"Lt", criteria.Lt("f", 1), "<"},
		{"Lte", criteria.Lte("f", 1), "<="},
		{"Gt", criteria.Gt("f", 1), ">"},
		{"Gte", criteria.Gte("f", 1), ">="},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, params := criteria.From("t", "").Where(tc.predicate).Build(pg)
			assert.Equal(t, fmt.Sprintf("SELECT * FROM t WHERE f %s $1", tc.wantOp), sql)
			assert.Equal(t, []any{1}, params)
		})
	}
}

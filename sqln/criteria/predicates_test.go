package criteria

// Internal test — uses package criteria directly to inspect unexported Predicate fields.

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi/sqln/driver"
	"github.com/stretchr/testify/assert"
)

// isLogical

func TestIsLogical_TrueForAnd(t *testing.T) {
	assert.True(t, And().isLogical())
}

func TestIsLogical_TrueForOr(t *testing.T) {
	assert.True(t, Or().isLogical())
}

func TestIsLogical_FalseForEq(t *testing.T) {
	assert.False(t, Eq("f", 1).isLogical())
}

func TestIsLogical_FalseForNe(t *testing.T) {
	assert.False(t, Ne("f", 1).isLogical())
}

func TestIsLogical_FalseForLt(t *testing.T) {
	assert.False(t, Lt("f", 1).isLogical())
}

func TestIsLogical_FalseForLte(t *testing.T) {
	assert.False(t, Lte("f", 1).isLogical())
}

func TestIsLogical_FalseForGt(t *testing.T) {
	assert.False(t, Gt("f", 1).isLogical())
}

func TestIsLogical_FalseForGte(t *testing.T) {
	assert.False(t, Gte("f", 1).isLogical())
}

func TestIsLogical_FalseForIn(t *testing.T) {
	assert.False(t, In("f", []int{1}).isLogical())
}

func TestIsLogical_FalseForNotIn(t *testing.T) {
	assert.False(t, NotIn("f", []int{1}).isLogical())
}

func TestIsLogical_FalseForContains(t *testing.T) {
	assert.False(t, Contains("f", "x").isLogical())
}

func TestIsLogical_FalseForNotContains(t *testing.T) {
	assert.False(t, NotContains("f", "x").isLogical())
}

func TestIsLogical_FalseForLike(t *testing.T) {
	assert.False(t, Like("f", "x").isLogical())
}

func TestIsLogical_FalseForNotLike(t *testing.T) {
	assert.False(t, NotLike("f", "x").isLogical())
}

func TestIsLogical_FalseForBetween(t *testing.T) {
	assert.False(t, Between("f", 1, 10).isLogical())
}

func TestIsLogical_FalseForIsNull(t *testing.T) {
	assert.False(t, IsNull("f").isLogical())
}

func TestIsLogical_FalseForIsNotNull(t *testing.T) {
	assert.False(t, IsNotNull("f").isLogical())
}

// Logical connectors

func TestAnd_LogicalFieldIsDriverAnd(t *testing.T) {
	p := And()
	assert.Equal(t, driver.And, p.logical)
	assert.Empty(t, p.field)
	assert.Empty(t, p.operator)
	assert.Nil(t, p.value)
}

func TestOr_LogicalFieldIsDriverOr(t *testing.T) {
	p := Or()
	assert.Equal(t, driver.Or, p.logical)
	assert.Empty(t, p.field)
	assert.Empty(t, p.operator)
	assert.Nil(t, p.value)
}

// Comparison constructors

func TestEq_Fields(t *testing.T) {
	p := Eq("u.active", true)
	assert.Equal(t, "u.active", p.field)
	assert.Equal(t, driver.Eq, p.operator)
	assert.Equal(t, true, p.value)
	assert.Empty(t, p.logical)
}

func TestNe_Fields(t *testing.T) {
	p := Ne("u.status", "banned")
	assert.Equal(t, "u.status", p.field)
	assert.Equal(t, driver.NotEqual, p.operator)
	assert.Equal(t, "banned", p.value)
}

func TestLt_Fields(t *testing.T) {
	p := Lt("p.price", 100)
	assert.Equal(t, "p.price", p.field)
	assert.Equal(t, driver.Less, p.operator)
	assert.Equal(t, 100, p.value)
}

func TestLte_Fields(t *testing.T) {
	p := Lte("p.price", 100)
	assert.Equal(t, driver.LessOrEqual, p.operator)
	assert.Equal(t, 100, p.value)
}

func TestGt_Fields(t *testing.T) {
	p := Gt("p.stock", 0)
	assert.Equal(t, driver.Greater, p.operator)
	assert.Equal(t, 0, p.value)
}

func TestGte_Fields(t *testing.T) {
	p := Gte("p.score", 4.5)
	assert.Equal(t, driver.GreaterOrEqual, p.operator)
	assert.Equal(t, 4.5, p.value)
}

// Membership constructors

func TestIn_Fields(t *testing.T) {
	vals := []string{"admin", "mod"}
	p := In("u.role", vals)
	assert.Equal(t, "u.role", p.field)
	assert.Equal(t, driver.In, p.operator)
	assert.Equal(t, vals, p.value)
}

func TestNotIn_Fields(t *testing.T) {
	vals := []string{"banned"}
	p := NotIn("u.status", vals)
	assert.Equal(t, "u.status", p.field)
	assert.Equal(t, driver.NotIn, p.operator)
	assert.Equal(t, vals, p.value)
}

// Text search constructors

func TestContains_Fields(t *testing.T) {
	p := Contains("u.name", "%john%")
	assert.Equal(t, "u.name", p.field)
	assert.Equal(t, driver.Contains, p.operator) // "LIKE" → dispatched to dialect.Like
	assert.Equal(t, "%john%", p.value)
}

func TestNotContains_Fields(t *testing.T) {
	p := NotContains("u.name", "%spam%")
	assert.Equal(t, driver.NotContains, p.operator)
	assert.Equal(t, "%spam%", p.value)
}

func TestLike_Fields(t *testing.T) {
	p := Like("u.code", "USR%")
	assert.Equal(t, "u.code", p.field)
	assert.Equal(t, driver.Like, p.operator) // "~~" — case-sensitive raw LIKE
	assert.Equal(t, "USR%", p.value)
}

func TestNotLike_Fields(t *testing.T) {
	p := NotLike("u.code", "TMP%")
	assert.Equal(t, driver.NotLike, p.operator)
	assert.Equal(t, "TMP%", p.value)
}

// Range constructor

func TestBetween_Fields(t *testing.T) {
	p := Between("o.total", 100, 500)
	assert.Equal(t, "o.total", p.field)
	assert.Equal(t, driver.Between, p.operator)
	assert.Equal(t, []any{100, 500}, p.value)
}

func TestBetween_ValueIsAlwaysTwoElementSlice(t *testing.T) {
	p := Between("f", "a", "z")
	vals, ok := p.value.([]any)
	assert.True(t, ok)
	assert.Len(t, vals, 2)
	assert.Equal(t, "a", vals[0])
	assert.Equal(t, "z", vals[1])
}

// Null check constructors

func TestIsNull_Fields(t *testing.T) {
	p := IsNull("u.deleted_at")
	assert.Equal(t, "u.deleted_at", p.field)
	assert.Equal(t, driver.IsNull, p.operator)
	assert.Nil(t, p.value) // no bound parameter
	assert.Empty(t, p.logical)
}

func TestIsNotNull_Fields(t *testing.T) {
	p := IsNotNull("u.confirmed_at")
	assert.Equal(t, "u.confirmed_at", p.field)
	assert.Equal(t, driver.IsNotNull, p.operator)
	assert.Nil(t, p.value)
}

// Boolean check constructors

func TestIsTrue_Fields(t *testing.T) {
	p := IsTrue("p.managed")
	assert.Equal(t, "p.managed", p.field)
	assert.Equal(t, driver.IsTrue, p.operator)
	assert.Nil(t, p.value)
	assert.Empty(t, p.logical)
	assert.False(t, p.isLogical())
}

func TestIsFalse_Fields(t *testing.T) {
	p := IsFalse("p.active")
	assert.Equal(t, "p.active", p.field)
	assert.Equal(t, driver.IsFalse, p.operator)
	assert.Nil(t, p.value)
	assert.Empty(t, p.logical)
	assert.False(t, p.isLogical())
}

// Group constructor

func TestGroup_Fields(t *testing.T) {
	p := Group(Eq("a", 1), Or(), Eq("b", 2))
	assert.Equal(t, driver.Group, p.operator)
	assert.Empty(t, p.field)
	assert.Nil(t, p.value)
	assert.Empty(t, p.logical)
	assert.False(t, p.isLogical())
	assert.Len(t, p.children, 3)
}

func TestGroup_EmptyChildren(t *testing.T) {
	p := Group()
	assert.Equal(t, driver.Group, p.operator)
	assert.Empty(t, p.children)
}

// Predicate is a value type

func TestPredicate_IsValueType_CopySafe(t *testing.T) {
	original := Eq("f", 42)
	copy := original
	// Mutating copy's logical field doesn't affect original.
	copy.logical = "AND"
	assert.Empty(t, original.logical)
}

// Table-driven: all operators map to correct driver constant

func TestPredicates_OperatorMapping(t *testing.T) {
	tests := []struct {
		name   string
		pred   Predicate
		wantOp string
	}{
		{"Eq", Eq("f", 0), driver.Eq},
		{"Ne", Ne("f", 0), driver.NotEqual},
		{"Lt", Lt("f", 0), driver.Less},
		{"Lte", Lte("f", 0), driver.LessOrEqual},
		{"Gt", Gt("f", 0), driver.Greater},
		{"Gte", Gte("f", 0), driver.GreaterOrEqual},
		{"In", In("f", []int{1}), driver.In},
		{"NotIn", NotIn("f", []int{1}), driver.NotIn},
		{"Contains", Contains("f", "x"), driver.Contains},
		{"NotContains", NotContains("f", "x"), driver.NotContains},
		{"Like", Like("f", "x"), driver.Like},
		{"NotLike", NotLike("f", "x"), driver.NotLike},
		{"Between", Between("f", 1, 2), driver.Between},
		{"IsNull", IsNull("f"), driver.IsNull},
		{"IsNotNull", IsNotNull("f"), driver.IsNotNull},
		{"IsTrue", IsTrue("f"), driver.IsTrue},
		{"IsFalse", IsFalse("f"), driver.IsFalse},
		{"DateEq", DateEq("f", time.Now()), driver.Eq},
		{"DateBefore", DateBefore("f", time.Now()), driver.Less},
		{"DateAfter", DateAfter("f", time.Now()), driver.Greater},
		{"DateOnOrBefore", DateOnOrBefore("f", time.Now()), driver.LessOrEqual},
		{"DateOnOrAfter", DateOnOrAfter("f", time.Now()), driver.GreaterOrEqual},
		{"DateBetween", DateBetween("f", time.Now(), time.Now().Add(time.Hour)), driver.Between},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantOp, tc.pred.operator)
			assert.False(t, tc.pred.isLogical())
		})
	}
}

// Date predicates

var (
	dateNow   = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	dateLater = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
)

func TestDateEq_Fields(t *testing.T) {
	p := DateEq("o.created_at", dateNow)
	assert.Equal(t, "o.created_at", p.field)
	assert.Equal(t, driver.Eq, p.operator)
	assert.Equal(t, dateNow, p.value)
	assert.Empty(t, p.logical)
	assert.False(t, p.isLogical())
}

func TestDateEq_PanicsOnZeroTime(t *testing.T) {
	assert.Panics(t, func() { DateEq("f", time.Time{}) })
}

func TestDateBefore_Fields(t *testing.T) {
	p := DateBefore("o.created_at", dateNow)
	assert.Equal(t, driver.Less, p.operator)
	assert.Equal(t, dateNow, p.value)
}

func TestDateBefore_PanicsOnZeroTime(t *testing.T) {
	assert.Panics(t, func() { DateBefore("f", time.Time{}) })
}

func TestDateAfter_Fields(t *testing.T) {
	p := DateAfter("o.created_at", dateNow)
	assert.Equal(t, driver.Greater, p.operator)
	assert.Equal(t, dateNow, p.value)
}

func TestDateAfter_PanicsOnZeroTime(t *testing.T) {
	assert.Panics(t, func() { DateAfter("f", time.Time{}) })
}

func TestDateOnOrBefore_Fields(t *testing.T) {
	p := DateOnOrBefore("o.created_at", dateNow)
	assert.Equal(t, driver.LessOrEqual, p.operator)
	assert.Equal(t, dateNow, p.value)
}

func TestDateOnOrBefore_PanicsOnZeroTime(t *testing.T) {
	assert.Panics(t, func() { DateOnOrBefore("f", time.Time{}) })
}

func TestDateOnOrAfter_Fields(t *testing.T) {
	p := DateOnOrAfter("o.created_at", dateNow)
	assert.Equal(t, driver.GreaterOrEqual, p.operator)
	assert.Equal(t, dateNow, p.value)
}

func TestDateOnOrAfter_PanicsOnZeroTime(t *testing.T) {
	assert.Panics(t, func() { DateOnOrAfter("f", time.Time{}) })
}

func TestDateBetween_Fields(t *testing.T) {
	p := DateBetween("o.created_at", dateNow, dateLater)
	assert.Equal(t, "o.created_at", p.field)
	assert.Equal(t, driver.Between, p.operator)
	vals, ok := p.value.([]any)
	assert.True(t, ok)
	assert.Len(t, vals, 2)
	assert.Equal(t, dateNow, vals[0])
	assert.Equal(t, dateLater, vals[1])
	assert.Empty(t, p.logical)
	assert.False(t, p.isLogical())
}

func TestDateBetween_SameDateAllowed(t *testing.T) {
	assert.NotPanics(t, func() { DateBetween("f", dateNow, dateNow) })
}

func TestDateBetween_PanicsWhenFromIsZero(t *testing.T) {
	assert.Panics(t, func() { DateBetween("f", time.Time{}, dateLater) })
}

func TestDateBetween_PanicsWhenToIsZero(t *testing.T) {
	assert.Panics(t, func() { DateBetween("f", dateNow, time.Time{}) })
}

func TestDateBetween_PanicsWhenFromAfterTo(t *testing.T) {
	assert.Panics(t, func() { DateBetween("f", dateLater, dateNow) })
}

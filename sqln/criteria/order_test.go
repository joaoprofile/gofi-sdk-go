package criteria_test

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/criteria"
	"github.com/stretchr/testify/assert"
)

//  Constants

func TestASC_ConstantValue(t *testing.T) {
	assert.Equal(t, "ASC", criteria.ASC)
}

func TestDESC_ConstantValue(t *testing.T) {
	assert.Equal(t, "DESC", criteria.DESC)
}

//  Asc

func TestAsc_SetsField(t *testing.T) {
	o := criteria.Asc("u.name")
	assert.Equal(t, "u.name", o.Field)
}

func TestAsc_SetsDirectionASC(t *testing.T) {
	o := criteria.Asc("u.name")
	assert.Equal(t, criteria.ASC, o.Direction)
}

func TestAsc_EmptyField(t *testing.T) {
	o := criteria.Asc("")
	assert.Equal(t, "", o.Field)
	assert.Equal(t, "ASC", o.Direction)
}

func TestAsc_FieldWithTableQualifier(t *testing.T) {
	o := criteria.Asc("orders.created_at")
	assert.Equal(t, "orders.created_at", o.Field)
}

//  Desc──

func TestDesc_SetsField(t *testing.T) {
	o := criteria.Desc("u.created_at")
	assert.Equal(t, "u.created_at", o.Field)
}

func TestDesc_SetsDirectionDESC(t *testing.T) {
	o := criteria.Desc("u.created_at")
	assert.Equal(t, criteria.DESC, o.Direction)
}

func TestDesc_EmptyField(t *testing.T) {
	o := criteria.Desc("")
	assert.Equal(t, "", o.Field)
	assert.Equal(t, "DESC", o.Direction)
}

//  Order struct

func TestOrder_FieldAndDirectionAreExported(t *testing.T) {
	// Ensures Order remains a value type with exported fields (API stability).
	o := criteria.Order{Field: "id", Direction: "ASC"}
	assert.Equal(t, "id", o.Field)
	assert.Equal(t, "ASC", o.Direction)
}

func TestAsc_ReturnsValueNotPointer(t *testing.T) {
	// Asc/Desc return value types — safe to copy.
	a := criteria.Asc("id")
	b := a
	b.Field = "name"
	assert.Equal(t, "id", a.Field) // original unchanged
}

// Interaction with Build

func TestAsc_ProducesAscClause(t *testing.T) {
	sql, _ := criteria.From("t", "").OrderBy(criteria.Asc("id")).Build(pgDialect{})
	assert.Contains(t, sql, "ORDER BY id ASC")
}

func TestDesc_ProducesDescClause(t *testing.T) {
	sql, _ := criteria.From("t", "").OrderBy(criteria.Desc("created_at")).Build(pgDialect{})
	assert.Contains(t, sql, "ORDER BY created_at DESC")
}

func TestOrderBy_MixedAscDesc_InOrder(t *testing.T) {
	sql, _ := criteria.From("t", "").
		OrderBy(criteria.Asc("name"), criteria.Desc("id")).
		Build(pgDialect{})
	assert.Equal(t, "SELECT * FROM t ORDER BY name ASC, id DESC", sql)
}

func TestOrderBy_MultipleCalls_Accumulate(t *testing.T) {
	sql, _ := criteria.From("t", "").
		OrderBy(criteria.Asc("name")).
		OrderBy(criteria.Desc("id")).
		Build(pgDialect{})
	assert.Contains(t, sql, "ORDER BY name ASC, id DESC")
}

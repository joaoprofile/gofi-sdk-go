package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func baseMapping() *QueryMapping {
	return &QueryMapping{
		AllowedFields: []FieldMapping{
			{Key: "name", Label: "Name", FilterType: "text", SearchType: "contains"},
			{Key: "age", Label: "Age", FilterType: "number", SearchType: "range"},
			{Key: "status", Label: "Status", FilterType: "select", SearchType: "exact"},
			{Key: "created_at", Label: "Created At", FilterType: "date", SearchType: "range"},
		},
		AllowedSortingFields: map[string]string{
			"name":       "name",
			"age":        "age",
			"created_at": "created_at",
		},
		Operators: map[string]string{
			"=":        "=",
			"!=":       "!=",
			"<":        "<",
			"<=":       "<=",
			">":        ">",
			">=":       ">=",
			"IN":       "IN",
			"NOT IN":   "NOT IN",
			"LIKE":     "LIKE",
			"NOT LIKE": "NOT LIKE",
			"BETWEEN":  "BETWEEN",
			"IS NULL":  "IS NULL",
		},
		LogicalOperators: map[string]string{And: And, Or: Or},
	}
}

func filtersFor(items ...*Filter) *Filters {
	return &Filters{Filters: items}
}

// Empty Filters

func TestValidate_EmptyFilters_NoError(t *testing.T) {
	assert.NoError(t, baseMapping().Validate(&Filters{}))
}

// Field Validation

func TestValidate_AllowedField_NoError(t *testing.T) {
	f := NewFilter("name", "=", "Emilia")
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

func TestValidate_UnknownField_ReturnsError(t *testing.T) {
	f := NewFilter("unknown_field", "=", "x")
	err := baseMapping().Validate(filtersFor(f))
	assert.ErrorContains(t, err, "invalid field")
}

// Condition Validation

func TestValidate_AllowedCondition_NoError(t *testing.T) {
	f := NewFilter("age", ">=", 18)
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

func TestValidate_UnknownCondition_ReturnsError(t *testing.T) {
	f := NewFilter("name", "INVALID_OP", "x")
	err := baseMapping().Validate(filtersFor(f))
	assert.ErrorContains(t, err, "invalid condition")
}

func TestValidate_EmptyCondition_NotValidatedAgainstOperators(t *testing.T) {
	f := NewFilter("name", "", "Emilia")
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

// Logical Operator Validation

func TestValidate_ValidLogicalOperator_AND(t *testing.T) {
	fs := filtersFor(NewFilter("name", "=", "Emilia"), AND(), NewFilter("age", ">=", 18))
	assert.NoError(t, baseMapping().Validate(fs))
}

func TestValidate_ValidLogicalOperator_OR(t *testing.T) {
	fs := filtersFor(NewFilter("name", "=", "Emilia"), OR(), NewFilter("name", "=", "bob"))
	assert.NoError(t, baseMapping().Validate(fs))
}

func TestValidate_InvalidLogicalOperator_ReturnsError(t *testing.T) {
	fs := filtersFor(NewFilter("name", "=", "Emilia"), &Filter{LogicalOperator: "XOR"}, NewFilter("age", "=", 1))
	err := baseMapping().Validate(fs)
	assert.ErrorContains(t, err, "invalid logical operator")
}

// Logical Operator Placement Validation

func TestValidate_LeadingLogicalOperator_ReturnsError(t *testing.T) {
	fs := filtersFor(AND(), NewFilter("name", "=", "Emilia"))
	err := baseMapping().Validate(fs)
	assert.ErrorContains(t, err, "cannot start with a logical operator")
}

func TestValidate_TrailingLogicalOperator_ReturnsError(t *testing.T) {
	fs := filtersFor(NewFilter("name", "=", "Emilia"), AND())
	err := baseMapping().Validate(fs)
	assert.ErrorContains(t, err, "cannot end with a logical operator")
}

func TestValidate_ConsecutiveLogicalOperators_ReturnsError(t *testing.T) {
	fs := filtersFor(NewFilter("name", "=", "Emilia"), AND(), OR(), NewFilter("age", "=", 1))
	err := baseMapping().Validate(fs)
	assert.ErrorContains(t, err, "consecutive logical operators")
}

// SQL Injection — String Values

func TestValidate_SQLInjection_StringValue_ReturnsError(t *testing.T) {
	cases := []string{
		"DROP TABLE users",
		"'; DELETE FROM accounts; --",
		"UNION SELECT * FROM secrets",
		"'; EXEC xp_cmdshell('rm -rf /')",
	}
	for _, c := range cases {
		f := NewFilter("name", "=", c)
		err := baseMapping().Validate(filtersFor(f))
		assert.ErrorContains(t, err, "action not allowed", "expected blocked: %s", c)
	}
}

func TestValidate_SafeStringValue_NoError(t *testing.T) {
	f := NewFilter("name", "=", "Emilia")
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

// SQL Injection — Slice Values

func TestValidate_SQLInjection_SliceValue_ReturnsError(t *testing.T) {
	f := NewFilter("status", "IN", []any{"active", "DROP TABLE users"})
	err := baseMapping().Validate(filtersFor(f))
	assert.ErrorContains(t, err, "action not allowed")
}

func TestValidate_SafeSliceValues_NoError(t *testing.T) {
	f := NewFilter("status", "IN", []any{"active", "inactive", "pending"})
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

func TestValidate_SliceWithNumericValues_NotChecked(t *testing.T) {
	f := NewFilter("age", "IN", []any{18, 25, 30})
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

// Nil Value

func TestValidate_NilValue_NoError(t *testing.T) {
	f := NewFilter("created_at", "IS NULL", nil)
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

// Non-string Value Not Checked for Injection

func TestValidate_IntValue_NotCheckedForInjection(t *testing.T) {
	f := NewFilter("age", ">", 25)
	assert.NoError(t, baseMapping().Validate(filtersFor(f)))
}

// Sort Field Validation

func TestValidate_ValidSortField_NoError(t *testing.T) {
	fs := &Filters{
		Filters: []*Filter{NewFilter("name", "=", "Emilia")},
		Params:  &FilterParams{SortField: "name"},
	}
	assert.NoError(t, baseMapping().Validate(fs))
}

func TestValidate_InvalidSortField_ReturnsError(t *testing.T) {
	fs := &Filters{
		Params: &FilterParams{SortField: "unknown_sort"},
	}
	err := baseMapping().Validate(fs)
	assert.ErrorContains(t, err, "invalid sorting field")
}

func TestValidate_EmptySortField_NoError(t *testing.T) {
	fs := &Filters{Params: &FilterParams{SortField: ""}}
	assert.NoError(t, baseMapping().Validate(fs))
}

// validateLogicalPlacement — empty slice returns nil immediately (defensive guard)
// Note: Validate() short-circuits before calling this for empty filters, so we
// invoke the method directly from within the package.
func TestValidateLogicalPlacement_EmptySlice_ReturnsNil(t *testing.T) {
	err := baseMapping().validateLogicalPlacement([]*Filter{})
	assert.NoError(t, err)
}

func TestValidate_CompleteValidScenario(t *testing.T) {
	fs := &Filters{
		Params: &FilterParams{SortField: "name", SortDirection: "ASC"},
		Filters: []*Filter{
			NewFilter("name", "=", "Emilia"),
			AND(),
			NewFilter("age", ">=", 18),
			AND(),
			NewFilter("status", "IN", []any{"active", "pending"}),
		},
	}
	assert.NoError(t, baseMapping().Validate(fs))
}

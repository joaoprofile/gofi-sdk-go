package filter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var orders = Mapping{
	"status":  {Column: "o.status", Ops: Equality},
	"created": {Column: "o.created_at", Ops: Range, Sortable: true},
	"name":    {Column: "c.name", Ops: Text},
}

func TestBuild_TranslatesNamesAndContinuesPlaceholders(t *testing.T) {
	fs := NewFilters().Add(NewFilter("status", Eq, "paid"), AND(), NewFilter("name", Contains, "silva"))
	qp, err := Build("SELECT * FROM orders o WHERE o.tenant_id = $1", []any{"t-1"}, fs, orders, pg)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM orders o WHERE o.tenant_id = $1 AND ( o.status = $2 AND c.name ILIKE $3 ESCAPE '!' )", qp.Query)
	assert.Equal(t, []any{"t-1", StringValue("paid"), "%silva%"}, qp.Params)
}

// A column outside the mapping cannot be used as a boolean oracle.
func TestBuild_RejectsUnmappedColumn(t *testing.T) {
	fs := NewFilters().Add(NewFilter("password_hash", Like, "a"))
	qp, err := Build("SELECT * FROM users u WHERE 1=1", nil, fs, orders, pg)
	assert.Nil(t, qp)
	assert.ErrorIs(t, err, ErrInvalidFilter)
	assert.ErrorContains(t, err, "password_hash")
}

func TestBuild_RejectsOperatorNotAllowedForField(t *testing.T) {
	fs := NewFilters().Add(NewFilter("status", Contains, "pa"))
	_, err := Build("SELECT 1 WHERE 1=1", nil, fs, orders, pg)
	assert.ErrorContains(t, err, `condition "LIKE" is not allowed for field "status"`)
}

func TestBuild_RejectsInvalidSets(t *testing.T) {
	tests := map[string]*Filters{
		"injected field":     NewFilters().Add(NewFilter("id = 1 OR 1=1 --", Eq, "x")),
		"unsupported op":     NewFilters().Add(NewFilter("name", "SIMILAR TO", "x")),
		"trailing logical":   NewFilters().Add(NewFilter("status", Eq, "a"), AND()),
		"leading logical":    NewFilters().Add(OR(), NewFilter("status", Eq, "a")),
		"consecutive logics": NewFilters().Add(NewFilter("status", Eq, "a"), AND(), OR(), NewFilter("status", Eq, "b")),
		"nil filter":         {Filters: []*Filter{nil}},
	}
	for name, fs := range tests {
		t.Run(name, func(t *testing.T) {
			qp, err := Build("SELECT 1 WHERE 1=1", nil, fs, orders, pg)
			assert.Nil(t, qp)
			assert.ErrorIs(t, err, ErrInvalidFilter)
		})
	}
}

func TestBuild_ReportsEveryProblem(t *testing.T) {
	fs := NewFilters().Add(NewFilter("a", Eq, 1), AND(), NewFilter("b", Eq, 2))
	_, err := Build("SELECT 1 WHERE 1=1", nil, fs, orders, pg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidFilter))
	assert.Contains(t, err.Error(), `"a"`)
	assert.Contains(t, err.Error(), `"b"`)
}

func TestBuild_NoFiltersKeepsBase(t *testing.T) {
	qp, err := Build("SELECT 1 WHERE x = $1", []any{1}, nil, orders, pg)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1 WHERE x = $1", qp.Query)
	assert.Equal(t, []any{1}, qp.Params)
}

func TestBuild_RejectsInvalidMappedColumn(t *testing.T) {
	bad := Mapping{"x": {Column: "1=1 OR a"}}
	_, err := Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("x", Eq, 1)), bad, pg)
	assert.ErrorIs(t, err, ErrInvalidFilter)
}

func TestSortColumn(t *testing.T) {
	col, err := orders.SortColumn("created")
	require.NoError(t, err)
	assert.Equal(t, "o.created_at", col)

	for _, f := range []string{"status", "password_hash", "o.created_at"} {
		_, err := orders.SortColumn(f)
		assert.ErrorIs(t, err, ErrInvalidFilter, "sort by %q must be rejected", f)
	}
	col, err = orders.SortColumn("")
	assert.NoError(t, err)
	assert.Empty(t, col)
}

func TestAllow_KeepsExistingNames(t *testing.T) {
	m := Allow("o.status", "o.total")
	fs := NewFilters().Add(NewFilter("o.status", Eq, "paid"))
	qp, err := Build("SELECT 1 WHERE 1=1", nil, fs, m, pg)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1 WHERE 1=1 AND ( o.status = $1 )", qp.Query)
	_, err = Build("SELECT 1 WHERE 1=1", nil, NewFilters().Add(NewFilter("o.secret", Eq, 1)), m, pg)
	assert.ErrorIs(t, err, ErrInvalidFilter)
}

// The column never reaches the JSON sent to a frontend.
func TestField_JSONHidesColumn(t *testing.T) {
	b, err := jsonMarshal(orders)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "o.created_at")
	assert.Contains(t, string(b), `"sortable":true`)
}

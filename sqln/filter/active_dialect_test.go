package filter

import (
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
)

func TestActiveDialect_PanicsWhenNoGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	assert.PanicsWithValue(t,
		"sqln/filter: no active database connection — call connection.SetGlobal before Build, or pass an explicit dialect",
		func() { activeDialect() },
	)
}

func TestBuild_NilDialectPanicsWithoutGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	fs := NewFilters().Add(NewFilter("name", Eq, "x"))
	assert.Panics(t, func() { _, _ = Build("SELECT 1 WHERE 1=1", nil, fs, allowAll(fs), nil) })
}

func TestBuild_ExplicitDialectNeedsNoGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	assert.NotPanics(t, func() {
		qp, err := Build("SELECT 1", nil, NewFilters(), nil, pg)
		assert.NoError(t, err)
		assert.NotNil(t, qp)
	})
}

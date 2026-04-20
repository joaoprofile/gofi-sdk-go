package filter

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/stretchr/testify/assert"
)

// activeDialect — panic path

func TestActiveDialect_PanicsWhenNoGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	assert.PanicsWithValue(t,
		"sqln/filter: no active database connection — call connection.SetGlobal before using NewQueryBuild, or use NewQueryBuildWithDialect with an explicit dialect",
		func() { activeDialect() },
	)
}

func TestNewQueryBuild_PanicsWhenNoGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	assert.Panics(t, func() {
		NewQueryBuild("SELECT 1", NewFilters())
	})
}

func TestNewQueryBuildWithDialect_NilDialect_PanicsWhenNoGlobalConnection(t *testing.T) {
	connection.ResetGlobalForTest()
	assert.Panics(t, func() {
		// nil dialect falls back to activeDialect() which panics without a global connection
		NewQueryBuildWithDialect("SELECT 1", NewFilters(), nil)
	})
}

func TestNewQueryBuildWithDialect_ExplicitDialect_NeverPanics(t *testing.T) {
	connection.ResetGlobalForTest()
	// Explicit dialect — activeDialect() is never called; no global connection required
	assert.NotPanics(t, func() {
		qp := NewQueryBuildWithDialect("SELECT 1", NewFilters(), pg)
		assert.NotNil(t, qp)
	})
}

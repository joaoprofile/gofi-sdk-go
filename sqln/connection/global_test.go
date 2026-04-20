package connection

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobal_NotInitialized(t *testing.T) {
	resetGlobal()

	conn, err := Global()
	assert.Nil(t, conn)
	assert.Error(t, err)
}

func TestGlobal_Initialized(t *testing.T) {
	resetGlobal()
	expected := newTestConnection("ok")
	SetGlobal(expected)

	conn, err := Global()
	require.NoError(t, err)
	assert.Equal(t, expected, conn)
}

func TestSetGlobal_OnlyFirstCallTakesEffect(t *testing.T) {
	resetGlobal()

	first := newTestConnection("ok")
	second := newTestConnection("ok")

	SetGlobal(first)
	SetGlobal(second) // deve ser ignorado

	conn, _ := Global()
	assert.Equal(t, first, conn)
}

func TestDB_NotInitialized(t *testing.T) {
	resetGlobal()

	db, err := DB()
	assert.Nil(t, db)
	assert.Error(t, err)
}

func TestDB_Initialized(t *testing.T) {
	resetGlobal()
	SetGlobal(newTestConnection("ok"))

	db, err := DB()
	require.NoError(t, err)
	assert.NotNil(t, db)
}

func TestMustDB_Panics_WhenNotInitialized(t *testing.T) {
	resetGlobal()

	assert.Panics(t, func() {
		MustDB()
	})
}

func TestMustDB_ReturnsDB_WhenInitialized(t *testing.T) {
	resetGlobal()
	SetGlobal(newTestConnection("ok"))

	assert.NotPanics(t, func() {
		db := MustDB()
		assert.NotNil(t, db)
	})
}

// Dialect

// TestDialect_ReturnsNil_WhenNoConnection verifies that Dialect() returns nil
// before any connection is established — callers must handle this case.
func TestDialect_ReturnsNil_WhenNoConnection(t *testing.T) {
	resetGlobal()

	d := Dialect()
	assert.Nil(t, d)
}

// TestDialect_ReturnsDriverDialect_AfterSetGlobal verifies that after
// SetGlobal is called, Dialect() delegates to the underlying driver's dialect.
// The fake driver in testhelpers returns fakeDialect whose Param always returns "?".
func TestDialect_ReturnsDriverDialect_AfterSetGlobal(t *testing.T) {
	resetGlobal()
	SetGlobal(newTestConnection("ok"))

	d := Dialect()

	require.NotNil(t, d)
	// fakeDialect always returns "?" regardless of index — confirms we got the
	// dialect from the registered fake driver, not some default.
	assert.Equal(t, "?", d.Param(1))
	assert.Equal(t, "?", d.Param(99))
}

// TestDialect_IsImmutableAfterSetGlobal verifies that a second SetGlobal call
// does not change the active dialect (singleton guarantee).
func TestDialect_IsImmutableAfterSetGlobal(t *testing.T) {
	resetGlobal()

	first := newTestConnection("ok")
	SetGlobal(first)

	dialectBefore := Dialect()

	// second SetGlobal must be silently ignored
	second := newTestConnection("ok")
	SetGlobal(second)

	dialectAfter := Dialect()

	assert.Equal(t, dialectBefore, dialectAfter)
}

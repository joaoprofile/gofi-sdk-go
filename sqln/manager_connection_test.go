package sqln

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/joaoprofile/gofi/sqln/pagination"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WithConnection — builder

func TestWithConnection_SetsConnection(t *testing.T) {
	setupGlobal(t, "ok")
	conn, err := connection.Global()
	require.NoError(t, err)

	m := Find[scalarItem](context.Background(), "SELECT 1").WithConnection(conn)
	assert.Equal(t, conn, m.conn)
}

func TestWithConnection_Chaining_ReturnsSameManager(t *testing.T) {
	setupGlobal(t, "ok")
	conn, err := connection.Global()
	require.NoError(t, err)

	m := Find[scalarItem](context.Background(), "SELECT 1")
	result := m.WithConnection(conn)
	assert.Same(t, m, result)
}

// connection() — injected takes precedence over global

func TestConnection_PrefersInjectedOverGlobal(t *testing.T) {
	setupGlobal(t, "no-rows") // global is "no-rows"

	initDriver()
	other, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "ok",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	m := Find[scalarItem](context.Background(), "SELECT 1").WithConnection(other)
	assert.Same(t, other, m.connection())
}

func TestConnection_FallsBackToGlobal_WhenNoneInjected(t *testing.T) {
	setupGlobal(t, "ok")
	global, err := connection.Global()
	require.NoError(t, err)

	m := Find[scalarItem](context.Background(), "SELECT 1")
	assert.Same(t, global, m.connection())
}

// High-level methods — injected connection, no global set

func TestList_WithInjectedConnection_NoGlobal(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "no-rows",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	connection.ResetGlobalForTest()

	list, err := Find[scalarItem](context.Background(), "SELECT id FROM t").
		WithConnection(conn).
		List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestUniqueResult_WithInjectedConnection_NoGlobal(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "no-rows",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	connection.ResetGlobalForTest()

	result, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").
		WithConnection(conn).
		UniqueResult()
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestPagedList_WithInjectedConnection_NoGlobal(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "count-rows",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	connection.ResetGlobalForTest()

	pr := pagination.NewPageRequest(0, 10, nil)
	result, err := Find[scalarItem](context.Background(), "SELECT id FROM t").
		WithConnection(conn).
		WithPage(pr).
		PagedList()
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecute_WithInjectedConnection_NoGlobal(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "no-rows",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	connection.ResetGlobalForTest()

	result, err := Find[mappedItem](context.Background(), "SELECT id, name FROM t").
		WithConnection(conn).
		Execute()
	require.NoError(t, err)
	assert.Nil(t, result)
}

// resolveFromCriteriaLazy — uses injected connection dialect, no global

func TestResolveFromCriteriaLazy_WithInjectedConnection_NoGlobal(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName),
		DSN:    "no-rows",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	connection.ResetGlobalForTest()

	// CriteriaFrom without predicates — verifies that the dialect is resolved
	// from the injected connection, not from connection.Global().
	q := CriteriaFrom("users", "u").Select("u.id")

	list, err := FindFromCriteria[scalarItem](context.Background(), q).
		WithConnection(conn).
		ExecuteListQuery(conn.DB())
	require.NoError(t, err)
	assert.Empty(t, list)
}

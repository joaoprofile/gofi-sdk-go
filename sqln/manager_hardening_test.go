package sqln

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: driver messages (tables, constraints, values) reached callers
// that returned err.Error() to API clients.
func TestManager_DriverErrorsAreGeneric(t *testing.T) {
	db := closedDB(t)
	_, err := Find[scalarItem](context.Background(), "SELECT id FROM t").ExecuteListQuery(db)
	require.Error(t, err)
	var dbErr *connection.Error
	require.ErrorAs(t, err, &dbErr)
	assert.Equal(t, "sqln: query failed", err.Error())
	assert.NotNil(t, connection.Cause(err))

	// Scan errors quote values: also wrapped.
	_, err = Find[mappedItem](context.Background(), "SELECT count FROM t").ExecuteUniqueResultQuery(openDB(t, "count-rows"))
	require.ErrorAs(t, err, &dbErr)
	assert.Equal(t, "scan", dbErr.Op)
}

// Regression: the client's limit (uint16) reached LIMIT unbounded.
func TestPageRequests_ClampLimit(t *testing.T) {
	fs := NewFilters()
	fs.Params.Limit = 65535
	pr, err := NewPageRequestFilter(fs, AllowColumns("id"))
	require.NoError(t, err)
	assert.Equal(t, DefaultMaxLimit, pr.Limit)

	pr, err = NewPageRequestFilter(fs, AllowColumns("id"), WithMaxLimit(500))
	require.NoError(t, err)
	assert.Equal(t, uint16(500), pr.Limit)

	assert.Equal(t, DefaultMaxLimit, NewPageRequest(0, 60000, nil).Limit)
	assert.Equal(t, DefaultLimit, NewPageRequest(0, 0, nil).Limit)
}

// Regression: a query without a ctx deadline could hold a connection forever.
func TestManager_QueryTimeout(t *testing.T) {
	initDriver()
	conn, err := connection.NewConnection(connection.Config{
		Driver: connection.DriverName(testDriverName), DSN: "block", QueryTimeout: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	defer conn.Close()

	start := time.Now()
	_, err = Find[scalarItem](context.Background(), "SELECT id FROM t").WithConnection(conn).List()
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)

	for _, err := range Find[scalarItem](context.Background(), "SELECT id FROM t").WithConnection(conn).All() {
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	}
}

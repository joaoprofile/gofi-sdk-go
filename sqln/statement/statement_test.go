package statement

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewStatement

func TestNewStatement_ReturnsNonNil(t *testing.T) {
	s := NewStatement()
	assert.NotNil(t, s)
}

// Execute

func TestExecute_EmptyQuery_ReturnsError(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	_, err := s.Execute(context.Background(), "")
	assert.ErrorContains(t, err, connection.ErrQueryIsEmpty)
}

func TestExecute_DBNotInitialized_ReturnsError(t *testing.T) {
	connection.ResetGlobalForTest()
	s := NewStatement()
	_, err := s.Execute(context.Background(), "SELECT 1")
	assert.ErrorContains(t, err, connection.ErrDatabaseNotInitialized)
}

func TestExecute_Success(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	_, err := s.Execute(context.Background(), "INSERT INTO t (id) VALUES (1)")
	assert.NoError(t, err)
}

func TestExecute_PrepareFails_ReturnsError(t *testing.T) {
	setupGlobalWithDSN(t, "fail-prepare")
	s := NewStatement()
	_, err := s.Execute(context.Background(), "SELECT 1")
	assert.EqualError(t, err, "sqln: exec failed", "the driver message stays out of Error()")
	assert.EqualError(t, connection.Cause(err), "prepare failed")
}

// Regression: statements without a context deadline could run forever.
func TestExecute_AppliesQueryTimeout(t *testing.T) {
	setupGlobalWithDSN(t, "record-deadline")
	_, err := NewStatement().Execute(context.Background(), "UPDATE t SET v = 1")
	require.NoError(t, err)
	had, _ := lastExecDeadline.Load("had")
	assert.Equal(t, true, had)
}

func TestPrepare_Fails_WrapsError(t *testing.T) {
	setupGlobalWithDSN(t, "fail-prepare")
	_, err := NewStatement().Prepare(context.Background(), "SELECT 1")
	var dbErr *connection.Error
	require.ErrorAs(t, err, &dbErr)
	assert.Equal(t, "prepare", dbErr.Op)
}

func TestExecute_InTransaction(t *testing.T) {
	setupGlobal(t)
	db := mustOpenDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	s := NewStatement()
	_, err := s.Execute(ctx, "INSERT INTO t (id) VALUES (1)")
	assert.NoError(t, err)
}

// Prepare

func TestPrepare_EmptyQuery_ReturnsError(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	stmt, err := s.Prepare(context.Background(), "")
	assert.Nil(t, stmt)
	assert.ErrorContains(t, err, connection.ErrQueryIsEmpty)
}

func TestPrepare_DBNotInitialized_ReturnsError(t *testing.T) {
	connection.ResetGlobalForTest()
	s := NewStatement()
	stmt, err := s.Prepare(context.Background(), "SELECT 1")
	assert.Nil(t, stmt)
	assert.ErrorContains(t, err, connection.ErrDatabaseNotInitialized)
}

func TestPrepare_Success(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	stmt, err := s.Prepare(context.Background(), "SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, stmt)
	_ = stmt.Close()
}

func TestPrepare_InTransaction(t *testing.T) {
	setupGlobal(t)
	db := mustOpenDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	s := NewStatement()
	stmt, err := s.Prepare(ctx, "SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, stmt)
	_ = stmt.Close()
}

// QueryRow

func TestQueryRow_DBNotInitialized_ReturnsError(t *testing.T) {
	connection.ResetGlobalForTest()
	row, err := NewStatement().QueryRow(context.Background(), "SELECT 1")
	assert.Nil(t, row)
	assert.ErrorContains(t, err, connection.ErrDatabaseNotInitialized)
}

func TestQueryRow_EmptyQuery_ReturnsError(t *testing.T) {
	setupGlobal(t)
	row, err := NewStatement().QueryRow(context.Background(), "")
	assert.Nil(t, row)
	assert.ErrorContains(t, err, connection.ErrQueryIsEmpty)
}

func TestQueryRow_Success(t *testing.T) {
	setupGlobal(t)
	row, err := NewStatement().QueryRow(context.Background(), "SELECT 1")
	require.NoError(t, err)
	assert.NotNil(t, row)
}

func TestQueryRow_InTransaction(t *testing.T) {
	setupGlobal(t)
	db := connection.MustDB()
	tx, err := db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := context.WithValue(context.Background(), connection.SqlTxContextKey, tx)

	row, err := NewStatement().QueryRow(ctx, "SELECT 1")
	require.NoError(t, err)
	assert.NotNil(t, row)
}

func TestExecute_ReturnsResult(t *testing.T) {
	setupGlobal(t)
	res, err := NewStatement().Execute(context.Background(), "UPDATE t SET v = 1")
	require.NoError(t, err)
	n, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

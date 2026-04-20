package statement

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
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
	err := s.Execute(context.Background(), "")
	assert.ErrorContains(t, err, connection.ErrQueryIsEmpty)
}

func TestExecute_DBNotInitialized_ReturnsError(t *testing.T) {
	connection.ResetGlobalForTest()
	s := NewStatement()
	err := s.Execute(context.Background(), "SELECT 1")
	assert.ErrorContains(t, err, connection.ErrDatabaseNotInitialized)
}

func TestExecute_Success(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	err := s.Execute(context.Background(), "INSERT INTO t (id) VALUES (1)")
	assert.NoError(t, err)
}

func TestExecute_PrepareFails_ReturnsError(t *testing.T) {
	setupGlobalWithDSN(t, "fail-prepare")
	s := NewStatement()
	err := s.Execute(context.Background(), "SELECT 1")
	assert.ErrorContains(t, err, "prepare failed")
}

func TestExecute_InTransaction(t *testing.T) {
	setupGlobal(t)
	db := mustOpenDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	s := NewStatement()
	err := s.Execute(ctx, "INSERT INTO t (id) VALUES (1)")
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

func TestQueryRow_DBNotInitialized_Panics(t *testing.T) {
	connection.ResetGlobalForTest()
	s := NewStatement()
	assert.Panics(t, func() {
		s.QueryRow(context.Background(), "SELECT 1")
	})
}

func TestQueryRow_EmptyQuery_ReturnsErrorRow(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	// empty query returns a row with the error message
	row := s.QueryRow(context.Background(), "")
	require.NotNil(t, row)
	// The fake driver returns a row; we just verify it doesn't panic
}

func TestQueryRow_Success(t *testing.T) {
	setupGlobal(t)
	s := NewStatement()
	row := s.QueryRow(context.Background(), "SELECT 1")
	assert.NotNil(t, row)
}

func TestQueryRow_InTransaction(t *testing.T) {
	setupGlobal(t)
	db := mustOpenDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	s := NewStatement()
	row := s.QueryRow(ctx, "SELECT 1")
	assert.NotNil(t, row)
}

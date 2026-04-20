package query

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuery_ReturnsNonNil(t *testing.T) {
	q := NewQuery()
	assert.NotNil(t, q)
}

// FetchRows — validation

func TestFetchRows_NilDB_ReturnsError(t *testing.T) {
	q := NewQuery()
	rows, err := q.FetchRows(context.Background(), nil, "SELECT 1")
	assert.Nil(t, rows)
	assert.ErrorContains(t, err, connection.ErrDatabaseNotInitialized)
}

func TestFetchRows_EmptyQuery_ReturnsError(t *testing.T) {
	db := openDB(t)
	q := NewQuery()
	rows, err := q.FetchRows(context.Background(), db, "")
	assert.Nil(t, rows)
	assert.ErrorContains(t, err, connection.ErrQueryIsEmpty)
}

// FetchRows — success paths

func TestFetchRows_Success(t *testing.T) {
	db := openDB(t)
	q := NewQuery()
	rows, err := q.FetchRows(context.Background(), db, "SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, rows)
	_ = rows.Close()
}

func TestFetchRows_InTransaction(t *testing.T) {
	db := openDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	q := NewQuery()
	rows, err := q.FetchRows(ctx, db, "SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, rows)
	_ = rows.Close()
}

// FetchRow

func TestFetchRow_Success(t *testing.T) {
	db := openDB(t)
	q := NewQuery()
	row := q.FetchRow(context.Background(), db, "SELECT 1")
	assert.NotNil(t, row)
}

func TestFetchRow_InTransaction(t *testing.T) {
	db := openDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	q := NewQuery()
	row := q.FetchRow(ctx, db, "SELECT 1")
	assert.NotNil(t, row)
}

// Execute

func TestExecute_Success(t *testing.T) {
	db := openDB(t)
	q := NewQuery()
	row := q.Execute(context.Background(), db, "SELECT 1")
	assert.NotNil(t, row)
}

func TestExecute_InTransaction(t *testing.T) {
	db := openDB(t)
	ctx, tx := txContext(t, db)
	defer tx.Rollback()

	q := NewQuery()
	row := q.Execute(ctx, db, "SELECT 1")
	assert.NotNil(t, row)
}

// SQLQuery struct

func TestSQLQuery_Fields(t *testing.T) {
	sq := SQLQuery{Query: "SELECT 1", Params: []any{42}}
	assert.Equal(t, "SELECT 1", sq.Query)
	assert.Equal(t, []any{42}, sq.Params)
}

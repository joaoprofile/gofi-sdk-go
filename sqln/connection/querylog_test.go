package connection

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubQuerier answers every call without a database.
type stubQuerier struct{ err error }

func (s stubQuerier) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, s.err
}
func (s stubQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, s.err
}
func (stubQuerier) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
func (s stubQuerier) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, s.err
}

func TestLogQueries(t *testing.T) {
	var buf bytes.Buffer
	LogQueries(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { LogQueries(nil) })
	ctx := context.Background()

	q := Logged(stubQuerier{})
	assert.Same(t, q, Logged(q), "wrapped once")
	_, _ = q.QueryContext(ctx, "SELECT p.id\n  FROM products p\n  WHERE p.name = $1 AND p.price <= $2 AND p.data = $3 AND p.x IS $4",
		"café", 100.5, []byte{1, 2}, nil)
	assert.Equal(t, "2000000", formatArg(2e6))
	out := buf.String()
	assert.Contains(t, out, `op=query`)
	assert.Contains(t, out, `sql="SELECT p.id FROM products p WHERE p.name = $1 AND p.price <= $2 AND p.data = $3 AND p.x IS $4"`, "one line")
	assert.Contains(t, out, `args="$1=\"café\" $2=100.5 $3=<2 bytes> $4=NULL"`)
	assert.Contains(t, out, "duration=")

	buf.Reset()
	_, _ = Logged(stubQuerier{err: errors.New("boom")}).ExecContext(ctx, "DELETE FROM t WHERE id = $1", strings.Repeat("x", 300))
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), `error=boom`)
	assert.Contains(t, buf.String(), strings.Repeat("x", maxArgLength)+`…`)
	assert.NotContains(t, buf.String(), strings.Repeat("x", maxArgLength+1))

	LogQueries(nil)
	buf.Reset()
	plain := Logged(stubQuerier{})
	_, _ = plain.QueryContext(ctx, "SELECT 1")
	assert.Empty(t, buf.String(), "off: nothing logged")
	_, isStub := plain.(stubQuerier)
	assert.True(t, isStub, "off: the querier is returned as is")
}

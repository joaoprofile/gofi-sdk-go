package connection

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatSQL(t *testing.T) {
	got := FormatSQL(`SELECT COUNT(*) FROM (SELECT p.id, COALESCE((SELECT string_agg(f.name, '|') FROM features f
		WHERE f.id = p.id), '') AS features FROM properties p JOIN cities c ON c.id = p.city_id
		LEFT JOIN LATERAL (SELECT 1) x ON TRUE WHERE p.active IS TRUE AND p.price BETWEEN $1 AND $2
		AND (p.a = $3 OR p.b = $4) AND p.v @@ to_tsquery('simple'::regconfig, $5)) tb`)
	assert.Equal(t, `SELECT
  COUNT(*)
FROM (
  SELECT
    p.id,
    COALESCE((
      SELECT
        string_agg(f.name, '|')
      FROM features f
      WHERE f.id = p.id
    ), '') AS features
  FROM properties p
    JOIN cities c ON c.id = p.city_id
    LEFT JOIN LATERAL (
      SELECT
        1
    ) x ON TRUE
  WHERE p.active IS TRUE
    AND p.price BETWEEN $1 AND $2
    AND (p.a = $3 OR p.b = $4)
    AND p.v @@ to_tsquery('simple'::regconfig, $5)
) tb`, got)

	assert.Equal(t, "SELECT\n  p.id\nFROM t p\nORDER BY p.id DESC\nLIMIT 10 OFFSET 20",
		FormatSQL("SELECT p.id FROM t p ORDER BY p.id DESC LIMIT 10 OFFSET 20"))
	assert.Equal(t, "INSERT INTO t(a, b)\nVALUES (?, 'it''s  (SELECT x)')\nRETURNING id",
		FormatSQL("INSERT INTO t (a, b) VALUES (?, 'it''s  (SELECT x)') RETURNING id"), "literals untouched")
}

// FormatSQL only moves whitespace outside literals: without whitespace, the
// statement is the same text.
func FuzzFormatSQL(f *testing.F) {
	for _, s := range []string{"SELECT a, b FROM t WHERE x = 'a  b' AND (y OR z)", "SELECT ((((", "'unterminated",
		`SELECT "Col"  FROM t`, "x::text", "a BETWEEN 1 AND 2 AND b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, q string) {
		squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
		if got := FormatSQL(q); squash(got) != squash(q) {
			t.Fatalf("FormatSQL(%q) = %q changes the text", q, got)
		}
	})
}

func TestPrintQueries(t *testing.T) {
	var buf bytes.Buffer
	PrintQueries(&buf)
	t.Cleanup(func() { PrintQueries(nil) })

	_, _ = Logged(stubQuerier{}).QueryContext(context.Background(), "SELECT p.id FROM t p WHERE p.a = $1 AND p.b = $2", "café", 3)
	out := buf.String()
	assert.Regexp(t, `^┌─ sql · query · \S+\n`, out)
	assert.Contains(t, out, "│ SELECT\n│   p.id\n│ FROM t p\n│ WHERE p.a = $1\n│   AND p.b = $2\n├─ args\n│   $1 = \"café\"\n│   $2 = 3\n└─\n")
	assert.NotContains(t, out, "\033[", "no colors outside a terminal")

	buf.Reset()
	_, _ = Logged(stubQuerier{err: errors.New("boom")}).ExecContext(context.Background(), "DELETE FROM t")
	assert.Contains(t, buf.String(), "├─ error: boom\n")

	PrintQueries(nil)
	buf.Reset()
	_, _ = Logged(stubQuerier{}).QueryContext(context.Background(), "SELECT 1")
	assert.Empty(t, buf.String())
}

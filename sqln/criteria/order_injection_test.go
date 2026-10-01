package criteria_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
)

func TestOrderBy_DirectionIsRestricted(t *testing.T) {
	sql, _ := built(t)(criteria.From("t", "").
		OrderBy(criteria.Order{Field: "id", Direction: "ASC, (SELECT pg_sleep(10))"}).
		Build(pgDialect{}))
	if strings.Contains(sql, "pg_sleep") || !strings.HasSuffix(sql, "ORDER BY id ASC") {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

// mysqlBlindSQLi closes a MySQL "…" string literal with \" and runs a timing subquery.
const mysqlBlindSQLi = `"a\".", (SELECT IF(SUBSTR(password,1,1)='a',SLEEP(1),0) FROM users LIMIT 1) -- "`

func TestBuild_RejectsRequestFieldsThatAreNotColumns(t *testing.T) {
	payloads := []string{mysqlBlindSQLi, "id; DROP TABLE users", "(SELECT 1)", "id--", "lower(name)", ""}
	for _, p := range payloads {
		queries := map[string]*criteria.Query{
			"order":   criteria.From("t", "").OrderBy(criteria.Asc(p)),
			"where":   criteria.From("t", "").Where(criteria.Eq(p, 1)),
			"isnull":  criteria.From("t", "").Where(criteria.IsNull(p)),
			"in":      criteria.From("t", "").Where(criteria.In(p, []any{1, 2})),
			"like":    criteria.From("t", "").Where(criteria.Contains(p, "x")),
			"between": criteria.From("t", "").Where(criteria.Between(p, 1, 2)),
			"group":   criteria.From("t", "").Where(criteria.Group(criteria.Eq("a", 1), criteria.Or(), criteria.Eq(p, 2))),
			"having":  criteria.From("t", "").Having(criteria.Gt(p, 0)),
		}
		for name, q := range queries {
			sql, params, err := q.Build(pgDialect{})
			if !errors.Is(err, criteria.ErrInvalidField) || sql != "" || params != nil {
				t.Errorf("%s(%q): sql=%q err=%v, want ErrInvalidField and no SQL", name, p, sql, err)
			}
			if _, _, err := q.BuildBase(pgDialect{}); name != "order" && !errors.Is(err, criteria.ErrInvalidField) {
				t.Errorf("BuildBase %s(%q): err=%v, want ErrInvalidField", name, p, err)
			}
		}
		if _, _, err := criteria.BuildClause([]criteria.Predicate{criteria.Eq(p, 1)}, pgDialect{}); !errors.Is(err, criteria.ErrInvalidField) {
			t.Errorf("BuildClause(%q): err=%v, want ErrInvalidField", p, err)
		}
	}
}

func TestBuild_RawMarksTrustedExpressions(t *testing.T) {
	sql, _ := built(t)(criteria.From("t", "").
		Having(criteria.Gt("COUNT(*)", 0).Raw()).
		OrderBy(criteria.Desc("COUNT(*)").Raw()).
		Build(pgDialect{}))
	if !strings.HasSuffix(sql, "HAVING COUNT(*) > $1 ORDER BY COUNT(*) DESC") {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

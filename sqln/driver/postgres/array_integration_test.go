package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
)

type status string

// Typed slices bound through = ANY / <> ALL reach PostgreSQL as arrays.
func TestIntegration_InBindsArrays(t *testing.T) {
	db := itConn(t).DB()
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := db.ExecContext(ctx, `CREATE TEMP TABLE it_any (id uuid, n bigint, s text, at timestamptz)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO it_any VALUES ($1, 7, 'paid', $2)`, id, now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]criteria.Predicate{
		"[]string":    criteria.In("s", []string{"paid", "open"}),
		"named":       criteria.In("s", []status{"paid"}),
		"[]int64":     criteria.In("n", []int64{7, 8}),
		"[]uuid.UUID": criteria.In("id", []uuid.UUID{id}),
		"[]time.Time": criteria.In("at", []time.Time{now}),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			where, params := built(t)(criteria.BuildClause([]criteria.Predicate{p}, postgres.PostgresDialect{}))
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM it_any WHERE "+where, params...).Scan(&count); err != nil {
				t.Fatalf("%s: %v", where, err)
			}
			if count != 1 {
				t.Errorf("%s matched %d rows", where, count)
			}
		})
	}
	where, params := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.NotIn("n", []int64{7})}, postgres.PostgresDialect{}))
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM it_any WHERE "+where, params...).Scan(&count); err != nil || count != 0 {
		t.Errorf("NOT IN: %d, %v", count, err)
	}
}

// built fails the test on a build error and returns the SQL and its parameters.
func built(t testing.TB) func(string, []any, error) (string, []any) {
	return func(sql string, params []any, err error) (string, []any) {
		t.Helper()
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return sql, params
	}
}

package postgres_test

import (
	"context"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/filter"
)

// Dynamic filters run on PostgreSQL with base-query args and mapped names.
func TestIntegration_FilterBuild(t *testing.T) {
	db := itConn(t).DB()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TEMP TABLE it_orders (tenant text, status text, total int, secret text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO it_orders VALUES ('a','paid',10,'s1'), ('a','open',20,'s2'), ('b','paid',30,'s3')`); err != nil {
		t.Fatal(err)
	}
	m := filter.Mapping{
		"status": {Column: "status", Ops: filter.Equality},
		"total":  {Column: "total", Ops: filter.Range},
	}
	fs := filter.NewFilters().Add(filter.NewFilter("status", filter.In, []string{"paid", "open"}), filter.AND(), filter.NewFilter("total", filter.GreaterOrEqual, 15))
	q, err := filter.Build("SELECT count(*) FROM it_orders WHERE tenant = $1", []any{"a"}, fs, m, postgres.PostgresDialect{})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, q.Query, q.Params...).Scan(&n); err != nil {
		t.Fatalf("%s %v: %v", q.Query, q.Params, err)
	}
	if n != 1 {
		t.Errorf("count=%d, want 1 (%s)", n, q.Query)
	}
	if _, err := filter.Build("SELECT 1 WHERE 1=1", nil, filter.NewFilters().Add(filter.NewFilter("secret", filter.Like, "s")), m, postgres.PostgresDialect{}); err == nil {
		t.Error("unmapped column must be rejected")
	}
}

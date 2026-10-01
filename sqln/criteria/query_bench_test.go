package criteria_test

import (
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/sqln/criteria"
)

func BenchmarkQueryBuild(b *testing.B) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = criteria.From("orders", "o").
			Select("o.id", "o.total", "c.name").
			Join("customers", "c", "c.id = o.customer_id").
			Where(
				criteria.Eq("o.status", "paid"),
				criteria.And(),
				criteria.In("o.region", []string{"br", "us", "eu"}),
				criteria.And(),
				criteria.DateOnOrAfter("o.created_at", since),
				criteria.And(),
				criteria.Contains("c.name", "silva"),
			).
			OrderBy(criteria.Desc("o.created_at")).
			Limit(50).Offset(100).
			Build(pgDialect{})
	}
}

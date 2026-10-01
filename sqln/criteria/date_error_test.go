package criteria_test

import (
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/criteria"
	"github.com/stretchr/testify/assert"
)

// Regression: an invalid date used to panic in the constructor; it must
// now surface as a Build error, including inside a group.
func TestBuildClause_InvalidDate_ReturnsErrInvalidValue(t *testing.T) {
	now := time.Now()
	cases := map[string]criteria.Predicate{
		"zero DateEq":          criteria.DateEq("created_at", time.Time{}),
		"zero DateBefore":      criteria.DateBefore("created_at", time.Time{}),
		"zero DateAfter":       criteria.DateAfter("created_at", time.Time{}),
		"zero DateOnOrBefore":  criteria.DateOnOrBefore("created_at", time.Time{}),
		"zero DateOnOrAfter":   criteria.DateOnOrAfter("created_at", time.Time{}),
		"zero DateBetween":     criteria.DateBetween("created_at", time.Time{}, now),
		"reversed DateBetween": criteria.DateBetween("created_at", now, now.Add(-time.Hour)),
		"nested in group": criteria.Group(
			criteria.Eq("status", "open"), criteria.Or(), criteria.DateEq("created_at", time.Time{}),
		),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			sql, params, err := criteria.BuildClause([]criteria.Predicate{criteria.Eq("id", 1), p}, pg)
			assert.ErrorIs(t, err, criteria.ErrInvalidValue)
			assert.Empty(t, sql)
			assert.Nil(t, params)
		})
	}
}

func TestQueryBuild_InvalidDate_ReturnsErrInvalidValue(t *testing.T) {
	q := criteria.From("orders", "o").Where(criteria.DateAfter("o.created_at", time.Time{}))
	_, _, err := q.Build(pg)
	assert.ErrorIs(t, err, criteria.ErrInvalidValue)
}

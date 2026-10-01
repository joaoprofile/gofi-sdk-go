package criteria_test

import (
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/criteria"
)

func TestBuildClause_EmptyMembership(t *testing.T) {
	in, args := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.In("id", []int{})}, pgDialect{}))
	if in != "1 = 0" || len(args) != 0 {
		t.Errorf("IN empty: %q %v", in, args)
	}
	notIn, _ := built(t)(criteria.BuildClause([]criteria.Predicate{criteria.NotIn("id", []string{})}, pgDialect{}))
	if notIn != "1 = 1" {
		t.Errorf("NOT IN empty: %q", notIn)
	}
}

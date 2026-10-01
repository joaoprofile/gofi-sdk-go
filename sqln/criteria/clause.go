package criteria

import (
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// BuildClause compiles a predicate slice into a SQL WHERE fragment and its bound parameters.
// It fails with ErrInvalidField when a field is not a column reference.
func BuildClause(predicates []Predicate, d driver.FilterDialect) (string, []any, error) {
	return BuildClauseAfter(nil, predicates, d)
}

// BuildClauseAfter numbers placeholders after args, the parameters the
// surrounding query already binds, and returns args followed by the new ones.
func BuildClauseAfter(args []any, predicates []Predicate, d driver.FilterDialect) (string, []any, error) {
	var sb strings.Builder
	b := newBuilder(d, &sb)
	b.params = append(make([]any, 0, len(args)+len(predicates)), args...)
	b.writeClause(predicates)
	if b.err != nil {
		return "", nil, b.err
	}
	return sb.String(), b.params, nil
}

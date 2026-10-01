package postgres

import (
	"fmt"
	"strconv"
)

type PostgresDialect struct{}

func (PostgresDialect) Param(index int) string {
	return "$" + strconv.Itoa(index)
}

func (PostgresDialect) Like(field string, param string) string {
	return field + " ILIKE " + param
}

func (PostgresDialect) NotLike(field string, param string) string {
	return field + " NOT ILIKE " + param
}

// ArrayMembership binds IN lists as one array: the SQL text is the same for
// any list length. <> ALL keeps NOT IN's NULL semantics.
func (PostgresDialect) ArrayMembership(field, param string, negate bool) string {
	if negate {
		return field + " <> ALL(" + param + ")"
	}
	return field + " = ANY(" + param + ")"
}

func (PostgresDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	if order == "" {
		return fmt.Sprintf("%s LIMIT %d OFFSET %d", query, limit, offset)
	}
	return fmt.Sprintf("%s ORDER BY %s LIMIT %d OFFSET %d", query, order, limit, offset)
}

// COUNT(*) rather than COUNT(tb.*): both count the same rows (an all-NULL row
// included), but naming the composite row forces the planner to materialize it,
// and it then gives up eliminating the unique-key LEFT JOINs nothing projects.
func (PostgresDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) tb", query)
}

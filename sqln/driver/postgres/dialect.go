package postgres

import "fmt"

type PostgresDialect struct{}

func (PostgresDialect) Param(index int) string {
	return fmt.Sprintf("$%d", index)
}

func (PostgresDialect) Like(field string, param string) string {
	return fmt.Sprintf("%s ILIKE %s", field, param)
}

func (PostgresDialect) NotLike(field string, param string) string {
	return fmt.Sprintf("%s NOT ILIKE %s", field, param)
}

func (PostgresDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	return fmt.Sprintf("%s ORDER BY %s LIMIT %d OFFSET %d", query, order, limit, offset)
}

// COUNT(*) rather than COUNT(tb.*): both count the same rows (an all-NULL row
// included), but naming the composite row forces the planner to materialize it,
// and it then gives up eliminating the unique-key LEFT JOINs nothing projects.
func (PostgresDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) tb", query)
}

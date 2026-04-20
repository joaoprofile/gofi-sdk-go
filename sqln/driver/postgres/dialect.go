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

func (PostgresDialect) BuildPagination(query string, order string, limit uint16, offset uint16) string {
	return fmt.Sprintf("%s ORDER BY %s LIMIT %d OFFSET %d", query, order, limit, offset)
}

func (PostgresDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(tb.*) FROM (%s) tb", query)
}

package mysql

import "fmt"

type MySQLDialect struct{}

func (MySQLDialect) Param(_ int) string {
	return "?"
}

func (MySQLDialect) Like(field string, param string) string {
	return field + " LIKE " + param
}

func (MySQLDialect) NotLike(field string, param string) string {
	return field + " NOT LIKE " + param
}

func (MySQLDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	if order == "" {
		return fmt.Sprintf("%s LIMIT %d OFFSET %d", query, limit, offset)
	}
	return fmt.Sprintf("%s ORDER BY %s LIMIT %d OFFSET %d", query, order, limit, offset)
}

func (MySQLDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) tb", query)
}

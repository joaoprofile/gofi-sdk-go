package sqlserver

import "fmt"

type SQLServerDialect struct{}

func (SQLServerDialect) Param(index int) string {
	return fmt.Sprintf("@p%d", index)
}

func (SQLServerDialect) Like(field string, param string) string {
	return fmt.Sprintf("%s LIKE %s", field, param)
}

func (SQLServerDialect) NotLike(field string, param string) string {
	return fmt.Sprintf("%s NOT LIKE %s", field, param)
}

func (SQLServerDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	return fmt.Sprintf(
		"%s ORDER BY %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY",
		query, order, offset, limit,
	)
}

func (SQLServerDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS tb", query)
}

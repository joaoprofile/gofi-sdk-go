package oracle

import "fmt"

type OracleDialect struct{}

func (OracleDialect) Param(index int) string {
	return fmt.Sprintf(":%d", index)
}

func (OracleDialect) Like(field string, param string) string {
	return fmt.Sprintf("%s LIKE %s", field, param)
}

func (OracleDialect) NotLike(field string, param string) string {
	return fmt.Sprintf("%s NOT LIKE %s", field, param)
}

func (OracleDialect) BuildPagination(query string, order string, limit uint16, offset uint16) string {
	return fmt.Sprintf(
		"SELECT * FROM (SELECT tb.*, ROWNUM rn FROM (%s ORDER BY %s) tb WHERE ROWNUM <= %d) WHERE rn > %d",
		query, order, offset+limit, offset,
	)
}

func (OracleDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) tb", query)
}

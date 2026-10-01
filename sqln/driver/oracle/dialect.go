package oracle

import (
	"fmt"
	"strconv"
)

type OracleDialect struct{}

func (OracleDialect) Param(index int) string {
	return ":" + strconv.Itoa(index)
}

func (OracleDialect) Like(field string, param string) string {
	return field + " LIKE " + param
}

func (OracleDialect) NotLike(field string, param string) string {
	return field + " NOT LIKE " + param
}

func (OracleDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	if order != "" {
		query += " ORDER BY " + order
	}
	return fmt.Sprintf(
		"SELECT * FROM (SELECT tb.*, ROWNUM rn FROM (%s) tb WHERE ROWNUM <= %d) WHERE rn > %d",
		query, offset+uint64(limit), offset,
	)
}

func (OracleDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) tb", query)
}

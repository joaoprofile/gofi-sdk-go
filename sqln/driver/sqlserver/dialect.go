package sqlserver

import (
	"fmt"
	"strconv"
)

type SQLServerDialect struct{}

func (SQLServerDialect) Param(index int) string {
	return "@p" + strconv.Itoa(index)
}

// LikeWildcards reports '[', which opens a character class in T-SQL LIKE.
func (SQLServerDialect) LikeWildcards() string { return "[" }

func (SQLServerDialect) Like(field string, param string) string {
	return field + " LIKE " + param
}

func (SQLServerDialect) NotLike(field string, param string) string {
	return field + " NOT LIKE " + param
}

func (SQLServerDialect) BuildPagination(query string, order string, limit uint16, offset uint64) string {
	if order == "" {
		order = "(SELECT NULL)" // OFFSET/FETCH requires an ORDER BY
	}
	return fmt.Sprintf(
		"%s ORDER BY %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY",
		query, order, offset, limit,
	)
}

func (SQLServerDialect) BuildCount(query string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS tb", query)
}

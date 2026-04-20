package driver

type FilterDialect interface {
	Param(index int) string
	Like(field string, param string) string
	NotLike(field string, param string) string
}

type Dialect interface {
	FilterDialect

	BuildPagination(query string, order string, limit uint16, offset uint16) string

	BuildCount(query string) string
}

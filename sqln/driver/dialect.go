package driver

type FilterDialect interface {
	Param(index int) string
	Like(field string, param string) string
	NotLike(field string, param string) string
}

type Dialect interface {
	FilterDialect

	// offset is uint64 because it is page*limit: two uint16 multiplied overflow
	// past 65535 and silently wrap to a different page.
	BuildPagination(query string, order string, limit uint16, offset uint64) string

	BuildCount(query string) string
}

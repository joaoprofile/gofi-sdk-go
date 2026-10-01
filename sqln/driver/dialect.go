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

// ArrayDialect is implemented by dialects that bind a whole slice as one
// array parameter, so IN lists keep the same SQL text for any length.
type ArrayDialect interface {
	// ArrayMembership renders field IN / NOT IN for an array placeholder.
	ArrayMembership(field, param string, negate bool) string
}

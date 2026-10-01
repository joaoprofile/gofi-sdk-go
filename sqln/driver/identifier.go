package driver

import (
	"regexp"
	"strings"
)

// segment: plain name, or a "…", `…` or […] quoted name. Quoted content is
// limited to letters, digits, '_', '$' and space: with no quote, backslash or
// punctuation inside, the segment cannot close early or be escaped in any
// dialect, even where the delimiter is a string literal (MySQL "…").
const segment = "(?:[A-Za-z_][A-Za-z0-9_$]*|\"[A-Za-z0-9_$ ]+\"|`[A-Za-z0-9_$ ]+`|\\[[A-Za-z0-9_$ ]+\\])"

var (
	identifierRe = regexp.MustCompile(`^` + segment + `(?:\.` + segment + `){0,2}$`)
	directionRe  = regexp.MustCompile(`(?i)^(ASC|DESC)(?:\s+NULLS\s+(FIRST|LAST))?$`)
)

// IsIdentifier reports whether s is a column reference such as col, t.col,
// "Col" or schema.t.col that is safe to write into SQL text from untrusted input.
func IsIdentifier(s string) bool { return identifierRe.MatchString(s) }

// IsSortDirection reports whether d is ASC/DESC with an optional NULLS FIRST/LAST.
func IsSortDirection(d string) bool { return directionRe.MatchString(strings.TrimSpace(d)) }

// SortDirection normalizes d; anything that is not a valid direction falls back to ASC.
func SortDirection(d string) string {
	m := directionRe.FindStringSubmatch(strings.TrimSpace(d))
	if m == nil {
		return "ASC"
	}
	out := strings.ToUpper(m[1])
	if m[2] != "" {
		out += " NULLS " + strings.ToUpper(m[2])
	}
	return out
}

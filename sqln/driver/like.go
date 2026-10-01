package driver

import "strings"

// LikeEscapeChar is the escape character of the ESCAPE clause written for
// literal LIKE matches. Unlike '\', '!' means the same inside a string
// literal in every dialect (MySQL escapes '\' unless NO_BACKSLASH_ESCAPES).
const LikeEscapeChar = "!"

// LikeEscapeClause follows a LIKE whose pattern was escaped with EscapeLike.
const LikeEscapeClause = " ESCAPE '" + LikeEscapeChar + "'"

// LikeWildcards is implemented by dialects whose LIKE has wildcard
// characters besides % and _, such as SQL Server's [...] classes.
type LikeWildcards interface {
	LikeWildcards() string
}

// EscapeLike escapes s so that LIKE … ESCAPE '!' matches it literally in d.
// Only the escape character, the wildcards and d's extra wildcards are
// escaped: Oracle rejects the escape character before anything else.
func EscapeLike(d FilterDialect, s string) string {
	special := LikeEscapeChar + "%_"
	if w, ok := d.(LikeWildcards); ok {
		special += w.LikeWildcards()
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteString(LikeEscapeChar)
		}
		b.WriteRune(r)
	}
	return b.String()
}

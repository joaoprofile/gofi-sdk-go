package driver

import (
	"strings"
	"testing"
)

func TestIsIdentifier(t *testing.T) {
	valid := []string{"id", "created_at", "u.name", "_col", "Col9", "schema_1.t_2", "s.t.c",
		`"createdAt"`, `u."Created At"`, "`order`", "[Order Date]", "col$1"}
	invalid := []string{"", "1col", "id;DROP TABLE users", "name DESC", "a.b.c.d", "(SELECT 1)", "id--",
		"u.", ".id", "na-me", "lower(name)", `"a"" OR 1=1 --"`, "`a` OR 1=1", "[a]] OR 1=1",
		// MySQL treats "…" as a string literal with \" escapes: the quoted segment must not carry them.
		`"a\".", (SELECT IF(SUBSTR(password,1,1)='a',SLEEP(1),0) FROM users LIMIT 1) -- "`,
		`"a\"`, `"a'b"`, `"a;b"`, `"a(b)"`, `"a-b"`, `"a/b"`, `"a*b"`, `"a,b"`, "\"a\nb\"", "\"a\tb\"",
		"`a\\`", "[a'b]", "[a--b]", `""`, "``", "[]", "\"ação\"", "col\x00"}
	for _, s := range valid {
		if !IsIdentifier(s) {
			t.Errorf("IsIdentifier(%q)=false, want true", s)
		}
	}
	for _, s := range invalid {
		if IsIdentifier(s) {
			t.Errorf("IsIdentifier(%q)=true, want false", s)
		}
	}
}

func TestSortDirection(t *testing.T) {
	tests := map[string]string{
		"ASC": "ASC", "desc": "DESC", " Desc ": "DESC", "": "ASC",
		"desc nulls last": "DESC NULLS LAST", "ASC  NULLS  FIRST": "ASC NULLS FIRST",
		"ASC, (SELECT pg_sleep(10))": "ASC", "DESC NULLS LAST; DROP": "ASC",
	}
	for in, want := range tests {
		if got := SortDirection(in); got != want {
			t.Errorf("SortDirection(%q)=%q, want %q", in, got, want)
		}
	}
}

// checkSafeIdentifier is an oracle independent of the regex: outside quoting only
// name characters and dots; inside a quoted segment only name characters and space.
func checkSafeIdentifier(s string) string {
	closer := map[byte]byte{'"': '"', '`': '`', '[': ']'}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(`'\;()-/*,#=<>+%`, c) >= 0 || c < 0x20 || c >= 0x7f {
			return "forbidden byte " + string(rune(c))
		}
		if end, ok := closer[c]; ok {
			j := strings.IndexByte(s[i+1:], end)
			if j <= 0 {
				return "unterminated or empty quoted segment"
			}
			for _, q := range []byte(s[i+1 : i+1+j]) {
				if !isNameByte(q) && q != ' ' {
					return "unsafe byte inside quotes"
				}
			}
			i += j + 1
			continue
		}
		if !isNameByte(c) && c != '.' {
			return "unsafe byte outside quotes"
		}
	}
	return ""
}

func isNameByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func FuzzIsIdentifier(f *testing.F) {
	for _, s := range []string{"id", "u.name", `"Created At"`, "`order`", "[Order Date]", `"a\".", (SELECT 1) -- "`,
		"id--", "a.b.c", `"a"" OR 1=1 --"`, "[a]] OR 1=1", "x\ny"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !IsIdentifier(s) {
			return
		}
		if why := checkSafeIdentifier(s); why != "" {
			t.Fatalf("IsIdentifier(%q)=true but %s", s, why)
		}
	})
}

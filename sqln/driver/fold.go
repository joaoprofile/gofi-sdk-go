package driver

import (
	"slices"
	"strings"
)

// accentFolds maps accented Latin letters to their base letter, as an _ai_
// collation and PostgreSQL's unaccent compare them.
var accentFolds = func() map[rune]rune {
	m := map[rune]rune{}
	for base, accented := range map[rune]string{
		'a': "àáâãäåā", 'c': "çćč", 'e': "èéêëēė", 'i': "ìíîïī", 'n': "ñń",
		'o': "òóôõöøō", 'u': "ùúûüū", 'y': "ýÿ", 's': "śš", 'z': "źżž",
	} {
		for _, r := range accented {
			m[r] = base
		}
	}
	return m
}()

// accentForms lists the accented forms of each base letter, sorted.
var accentForms = func() map[rune]string {
	m := map[rune]string{}
	for r, base := range accentFolds {
		m[base] += string(r)
	}
	for base, forms := range m {
		rs := []rune(forms)
		slices.Sort(rs)
		m[base] = string(rs)
	}
	return m
}()

// FoldAccents removes the accents of the Latin letters of a lower-case s.
func FoldAccents(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r >= 0xC0 }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if f, ok := accentFolds[r]; ok {
			r = f
		}
		b.WriteRune(r)
	}
	return b.String()
}

// AccentForms returns the base letter of r and its accented forms ("aàá…"),
// or "" when r has none.
func AccentForms(r rune) string {
	base := r
	if f, ok := accentFolds[r]; ok {
		base = f
	}
	if forms, ok := accentForms[base]; ok {
		return string(base) + forms
	}
	return ""
}

package fts

import (
	"html"
	"strings"
	"unicode"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// Highlight is a text whose matched words are wrapped in the private-use
// markers driver.HighlightStart and driver.HighlightEnd, as
// criteria.Query.SelectHighlight returns it. Scan it into a field of this
// type and render it with HTML or Fragments: never write it to a page as is.
// The text comes from the database unescaped, so it may hold markup.
type Highlight string

// Fragment is a run of text, matched or not.
type Fragment struct {
	Text  string
	Match bool
}

// Fragments splits h into matched and unmatched runs, without markers.
func (h Highlight) Fragments() []Fragment {
	var out []Fragment
	var b strings.Builder
	match := false
	flush := func() {
		if b.Len() > 0 {
			out = append(out, Fragment{Text: b.String(), Match: match})
			b.Reset()
		}
	}
	for _, r := range string(h) {
		switch string(r) {
		case driver.HighlightStart:
			flush()
			match = true
		case driver.HighlightEnd:
			flush()
			match = false
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// Text returns h without markers.
func (h Highlight) Text() string {
	return strings.NewReplacer(driver.HighlightStart, "", driver.HighlightEnd, "").Replace(string(h))
}

// HTML escapes the text and wraps each match in <mark>…</mark>: the only
// markup in the result is the one HTML adds.
func (h Highlight) HTML() string {
	var b strings.Builder
	for _, f := range h.Fragments() {
		if f.Match {
			b.WriteString("<mark>" + html.EscapeString(f.Text) + "</mark>")
		} else {
			b.WriteString(html.EscapeString(f.Text))
		}
	}
	return b.String()
}

// Excerpt cuts h to about maxWords words around its first match (the start
// of the text when nothing matched), marking each cut with "…". Whitespace
// collapses to single spaces.
func (h Highlight) Excerpt(maxWords int) Highlight {
	words := strings.FieldsFunc(string(h), unicode.IsSpace)
	if maxWords <= 0 || len(words) <= maxWords {
		return Highlight(strings.Join(words, " "))
	}
	first := 0
	for i, w := range words {
		if strings.Contains(w, driver.HighlightStart) {
			first = i
			break
		}
	}
	start := max(0, min(first-maxWords/3, len(words)-maxWords))
	end := start + maxWords
	out := strings.Join(words[start:end], " ")
	// A cut inside a match keeps it balanced.
	if strings.Count(out, driver.HighlightStart) > strings.Count(out, driver.HighlightEnd) {
		out += driver.HighlightEnd
	}
	if start > 0 {
		out = "… " + out
	}
	if end < len(words) {
		out += " …"
	}
	return Highlight(out)
}

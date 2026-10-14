package fts_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

const s, e = "", ""

func TestHighlight(t *testing.T) {
	h := fts.Highlight(s + "Café" + e + " <b>especial</b> & " + s + "café" + e + " moído")
	assert.Equal(t, []fts.Fragment{
		{Text: "Café", Match: true}, {Text: " <b>especial</b> & "}, {Text: "café", Match: true}, {Text: " moído"},
	}, h.Fragments())
	assert.Equal(t, "Café <b>especial</b> & café moído", h.Text())
	assert.Equal(t, "<mark>Café</mark> &lt;b&gt;especial&lt;/b&gt; &amp; <mark>café</mark> moído", h.HTML())

	assert.Equal(t, "plain", fts.Highlight("plain").HTML())
	assert.Empty(t, fts.Highlight("").Fragments())
	// Stray markers in the data never open markup of their own.
	assert.Equal(t, "a<mark>&lt;script&gt;</mark>", fts.Highlight("a"+s+"<script>").HTML())
}

func TestHighlightExcerpt(t *testing.T) {
	long := strings.Repeat("palavra ", 20) + s + "café" + e + strings.Repeat(" texto", 20)
	got := fts.Highlight(long).Excerpt(9)
	assert.Equal(t, "… palavra palavra palavra "+s+"café"+e+" texto texto texto texto texto …", string(got))
	assert.Equal(t, "um dois …", string(fts.Highlight("um  dois\ntres quatro").Excerpt(2)), "no match: the start")
	assert.Equal(t, "um dois", string(fts.Highlight("um  dois").Excerpt(5)))
	// A cut inside a matched phrase stays balanced.
	cut := fts.Highlight("a b " + s + "c d e" + e + " f").Excerpt(3)
	assert.Equal(t, "… b "+s+"c d"+e+" …", string(cut))
}

func FuzzHighlightHTML(f *testing.F) {
	for _, in := range []string{"<script>", s + "<img src=x onerror=1>" + e, "a&b\"'", e + "<" + s + ">"} {
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := strings.NewReplacer("<mark>", "", "</mark>", "").Replace(fts.Highlight(in).HTML())
		if strings.ContainsAny(out, "<>") {
			t.Fatalf("HTML(%q) = %q keeps markup", in, out)
		}
	})
}

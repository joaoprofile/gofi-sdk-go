// Package property is the real estate catalog: its full-text index, the
// interpreter that turns a free sentence into filters, the search and the
// listing of new properties.
package property

import "github.com/joaoprofile/gofi-sdk-go/sqln/fts"

//go:generate go test -run TestMigrationIsCurrent -update .

// searchIndex covers the title, the description and the text the
// repository copies from the related tables (location, type, offer,
// condition, advertiser and features), so one index searches all of them.
// Declared with MustIndex, it is checked when the service starts.
var searchIndex = fts.MustIndex("properties", fts.Spec{
	Name: "ftx_properties_search",
	Columns: []fts.Column{
		{Name: "title", Weight: fts.A},
		{Name: "location_text", Weight: fts.A},
		{Name: "facets_text", Weight: fts.B},
		{Name: "features_text", Weight: fts.B},
		{Name: "description", Weight: fts.C},
	},
	Language: fts.Portuguese,
	Unaccent: true, // "agua verde" finds "Água Verde"
	Suggest:  true, // "qurtos" → "quartos"
	Synonyms: [][]string{
		{"perto", "proximo"},
		{"garagem", "vaga"},
		{"academia", "fitness"},
		{"praia", "mar"},
		{"pet", "cachorro", "gato"},
		{"apartamento", "apto"},
		{"chacara", "sitio"},
	},
})

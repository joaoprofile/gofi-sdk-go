# sqln — real estate search

A property catalog searched like a web search engine: **one text box**. Type
everything you want in one sentence:

> Casa nova de 2 qurtos, suite, garagem 2 carros, curitiba, pr, bairro batel

and the service reads it as a typo fix, filters and a full-text search:

| Typed | Understood as |
|-------|---------------|
| `qurtos` | corrected to `quartos` (*"Mostrando resultados para…"*) |
| `Casa` `nova` | type = casa, condition = novo |
| `2 quartos` | exactly 2 bedrooms |
| `suite` | at least 1 suite |
| `garagem 2 carros` | at least 2 parking spaces |
| `curitiba` `pr` `bairro batel` | the Batel neighborhood of Curitiba/PR |
| anything else | full-text search over title, description, location and features |

```
realestate/
├── .env, compose.yaml          # PostgreSQL on localhost:5435
├── .migrations/
│   ├── 000001_create_schema.*  # states, cities, neighborhoods, types, offers,
│   │                           # conditions, owners, features, properties
│   ├── 000002_seed.*           # 117 sample properties
│   └── 000003_property_search.*# rendered from searchIndex: do not edit
├── main.go                     # gofi: database + HTTP server
├── property/
│   ├── index.go                # the fts.Index (weights, synonyms, Suggest)
│   ├── interpret.go            # sentence → filters + free words
│   ├── search.go               # typo fix, query, relaxation, highlight
│   ├── create.go               # POST /api/properties
│   ├── handler.go, service.go, dictionary.go, format.go
│   └── *_test.go               # interpreter cases, migration up to date
└── web/                        # index.html, app.js, app.css (embedded)
```

## Run

```sh
cd examples/sqln/realestate
docker compose up -d     # PostgreSQL on localhost:5435
go run .                 # http://localhost:8080 — Ctrl+C to stop
docker compose down -v   # when you are done
```

Open <http://localhost:8080> and try the examples under the box, or:

```sh
curl -G localhost:8080/api/search --data-urlencode 'q=apartamento para alugar em são paulo até 8 mil com academia'
```

## How a sentence is searched

1. **Interpret.** The words are matched, longest first, against the reference
   tables loaded at startup (neighborhoods, cities, states, types, offers,
   conditions, owner types, features) and their aliases (`apto`, `alugar`,
   `na planta`, `direto com o dono`…). Numbers take the unit next to them:
   `2 quartos`, `garagem para 2`, `100m²`, `até 900 mil`, `a partir de R$ 3.000`,
   `pelo menos 3 quartos`. A two-letter state that is also a word (`se`, `to`)
   counts only after a comma or in capitals.
2. **Correct.** Words left over may be misspelled names: `fts.Suggest` (via
   `sqln.SuggestSearch`) replaces them with the closest word of the catalog's
   lexicon, and the corrected sentence is interpreted again.
3. **Search.** Filters become `criteria` predicates; the remaining words and
   the required features (as phrases) become one `criteria.Match` on the
   full-text index, ranked by `SelectRelevance`, with `SelectHighlight` on the
   title and description.
4. **Relax.** When nothing matches, one condition at a time is loosened —
   features, then some of the words, then the words, price and area, the exact
   number of bedrooms, the counts, new/used and advertiser, and finally the
   neighborhood for its whole city. Type, offer and place stay. The page says
   what was loosened.

### The index

```go
var searchIndex = fts.MustIndex("properties", fts.Spec{
    Name: "ftx_properties_search",
    Columns: []fts.Column{
        {Name: "title", Weight: fts.A},
        {Name: "location_text", Weight: fts.A},  // "Batel Curitiba Paraná PR"
        {Name: "facets_text", Weight: fts.B},    // "Casa Venda Novo Imobiliária"
        {Name: "features_text", Weight: fts.B},  // "Churrasqueira Jardim Quintal"
        {Name: "description", Weight: fts.C},
    },
    Language: fts.Portuguese,
    Unaccent: true,
    Suggest:  true,
    Synonyms: [][]string{{"perto", "proximo"}, {"garagem", "vaga"}, …},
})
```

A full-text index reads one table, so the text of the related rows
(neighborhood, city, type, features…) is copied into `*_text` columns of
`properties` — by the seed, and by `Create` in the same transaction as the
listing. `go generate ./...` rewrites the index migration; the test fails when
it is out of date. The service does not start without the index
(`database.New()` checks it).

## The SQL it runs

`DATABASE_LOG_QUERIES=true` (on in `.env`; `false` turns it off) logs every
statement sqln sends to the database — the lexicon lookup of the typo fix, the
`COUNT` and the page of each attempt — with its arguments and duration:

```
level=INFO msg=sql op=query sql="SELECT COUNT(*) FROM (SELECT p.id, … WHERE p.active IS TRUE
  AND p.property_type = ANY($4) AND p.ftx_properties_search_tsv @@ to_tsquery(…, $5)) tb"
  duration=1.2ms args="$1=\"'parque' & 'barigui'\" … $4=[apartamento] …"
```

`DATABASE_LOG_QUERIES_PRETTY=true` prints the same statements to stderr laid
out for reading — colored in a terminal:

```
┌─ sql · query · 6.3ms
│ SELECT
│   COUNT(*)
│ FROM (
│   SELECT
│     p.id,
│     …
│   FROM properties p
│     JOIN neighborhoods n ON n.id = p.neighborhood_id
│     …
│   WHERE p.active IS TRUE
│     AND p.property_type = ANY($4)
│     AND p.bedrooms = $7
│     AND p.price <= $8
│     AND p.ftx_properties_search_tsv @@ to_tsquery('gofi_portuguese_unaccent'::regconfig, $9)
│ ) tb
├─ args
│   $4 = [casa]
│   $7 = 2
│   $8 = 2000000
│   $9 = "'piscina'"
└─
```

Both are refused in `stage` and `prod`: the arguments may hold personal data.
In code, `connection.LogQueries(logger)` and `connection.PrintQueries(os.Stderr)`
turn them on, and `nil` off.

## API

| Route | |
|-------|---|
| `GET /api/search?q=…&page=0` | results, `understood` chips, `corrected`, `approximate` |
| `GET /api/dictionary` | the codes and ids a listing refers to |
| `POST /api/properties` | lists a property; answers `{"id": …}` |

```sh
curl -X POST localhost:8080/api/properties -H 'Content-Type: application/json' -d '{
  "title": "Sobrado geminado com edícula no Cabral",
  "description": "Sobrado reformado, com edícula independente nos fundos.",
  "propertyType": "sobrado", "offerType": "venda", "condition": "usado",
  "ownerId": 1, "neighborhoodId": 6, "street": "Rua Fernando Amaro, 400",
  "bedrooms": 3, "suites": 1, "bathrooms": 3, "parkingSpaces": 2,
  "areaM2": 180, "price": 980000, "features": ["churrasqueira", "quintal"]}'
```

The listing is searchable at once, and its new words enter the lexicon:
`sobrado com edcula` is corrected to `edícula`. Here the lexicon is rebuilt
after each listing; a large catalog runs `fts.RefreshLexicon` from a job.

## The page

`web/` is plain HTML, CSS and JavaScript, embedded in the binary. The server's
Content-Security-Policy refuses inline scripts and styles, so they are files.
Results are written as text, except the title and snippet, which the server
renders with `fts.Highlight.HTML()`: the listing's text escaped, with only
`<mark>` around the matches. It searches as you type, keeps the search in the
URL (`/?q=…`), and follows the system's light or dark theme.

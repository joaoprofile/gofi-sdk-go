# sqln — full-text search job

A **job** (no HTTP server): it opens PostgreSQL through `gofi`, applies the
migrations (the catalog, then its full-text index), runs a few full-text
searches, prints the results and exits.

```
fulltext/
├── .env                      # DATABASE_* (read automatically outside prod/stage)
├── .migrations/
│   ├── 000001_create_catalog.*   # schema + seed data
│   └── 000002_products_search.*  # rendered from searchIndex: do not edit
├── compose.yaml              # PostgreSQL on localhost:5433
├── main.go                   # gofi.New().With(database.New()) → searches → Shutdown
└── product/
    ├── search.go             # the fts.Index and the ranked, paged search
    └── migration_test.go     # fails when the index changed and its migration did not
```

## Run

```sh
cd examples/sqln/fulltext
docker compose up -d     # PostgreSQL on localhost:5433
go run .
docker compose down      # when you are done
```

Output:

```
== Words, without accents: "cafe eletrica" ==
  1  Cafeteira Elétrica           0.1905
  8  Chaleira Elétrica            0.0816
-- 2 found

== Ranked: "cafe" (in the name ranks above the description) ==
  2  Café em Grãos Especial       1.4000
  3  Moedor de Café Manual        1.4000
  1  Cafeteira Elétrica           0.4000
...
== Phrase and exclusion: `"cafe coado" -moedor` ==
  8  Chaleira Elétrica            0.4000
-- 1 found
...
== Search as you type: "note ultra" ==
  5  Notebook Ultrafino           1.0000
-- 1 found
```

## How the index is enforced

There is no full-text API taking a column name: a search exists only on an
`fts.Index`, and the index reaches the database through a migration rendered
from that same definition.

| When | What fails | How |
|------|------------|-----|
| `go test` | The index changed, its migration did not | `migration_test.go` compares `.migrations/000002_*` with `fts.Migration`; `go generate ./...` rewrites them |
| Service startup | The migration was not applied, or was applied from another definition | `fts.MustIndex` registers the index; `database.New()` reads its comment (`gofi-fts:v1:<hash of the DDL>`) from the catalog right after the migrations: `fts.ErrIndexMissing` / `fts.ErrIndexOutdated` stop the service |
| Every search | Same, for indexes built at run time (`fts.NewIndex`) | The connection checks each index once before its first search; the search never runs without it |

Declare the index at package level, in the package that searches it (here
the repository): any package linked into the binary registers its indexes,
nothing to list in `main`. With several databases,
`database.New(database.WithFullText(idx...))` checks only the given ones.

`DATABASE_FULLTEXT_POLICY` selects `verify` (default), `ensure` (creates a
missing index — refused in `stage`/`prod`) or `trust` (no check, for mocked
databases).

## What it shows

| Search | sqln API |
|--------|----------|
| Index definition (weights, language, accent folding), checked at startup | `fts.MustIndex`, `fts.Spec` |
| Migration from the definition | `fts.WriteMigration` / `fts.Migration` |
| Match, rank, page by relevance | `criteria.Match`, `Query.SelectRelevance`, `pagination.NewSort("relevance", DESC)` |
| `cafe eletrica`, `"phrase"`, `-word`, `a OR b` | `fts.Websearch` (default) |
| Search as you type | `.Mode(fts.Prefix)` |
| Only stopwords or punctuation | `fts.ErrEmptySearch` → empty page |

## PostgreSQL and MySQL

The same `fts.Spec` and the same queries run on both; `DATABASE_DRIVER=mysql`
only changes what is rendered.

| | PostgreSQL | MySQL |
|---|---|---|
| Match | `vector @@ to_tsquery(...)`, GIN index | `MATCH(...) AGAINST (? IN BOOLEAN MODE)`, `FULLTEXT` index |
| Weights A–D | `setweight` labels, `ts_rank_cd` | one `FULLTEXT` index per weight group, relevance summed with the same factors (1.0, 0.4, 0.2, 0.1) |
| Accents | `unaccent` text search configuration | accent-insensitive collation, required by the check |
| Stopwords | the language's list | the same list (PostgreSQL's), plus InnoDB's own |
| Stemming | yes | no: `cafés` does not find `café` |
| Build without blocking writes | `Concurrently: true` (with `ExpressionIndex`) | not possible: the migration refuses it |

Words below `innodb_ft_min_token_size` are dropped from MySQL searches, and
`DATABASE_MIGRATION=true` works the same: sqln enables `multiStatements` on the
migration pool only, never on the query pools. Relevance values are not
comparable between the databases; the order they produce is.

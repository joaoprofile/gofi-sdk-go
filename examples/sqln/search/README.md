# sqln — search job

A **job** (no HTTP server): it opens PostgreSQL through `gofi`, applies the
migrations, runs a few `sqln` criteria queries, prints the results and exits.

```
search/
├── .env                 # DATABASE_* (read automatically outside prod/stage)
├── .migrations/         # schema + seed data, applied at startup
├── compose.yaml         # PostgreSQL
├── main.go              # gofi.New().With(database.New()) → run queries → Shutdown
└── product/
    └── repository.go    # criteria queries
```

## Run

```sh
cd examples/sqln/search
docker compose up -d     # PostgreSQL on localhost:5432
go run .
docker compose down      # when you are done
```

Output (abridged):

```
== Electronics and books up to 400, cheapest first ==
-- page 1 of 3 (7 products in total)
  2  Wireless Mouse                           electronics      129.90
  3  USB-C Hub                                electronics      199.00
 10  Clean Architecture                       books            199.90
...
== Catalog value per category (streamed) ==
books           1216.90
electronics     3576.80
office          1627.80
```

## What it shows

| Query | sqln API |
|-------|----------|
| Optional filters, join, sort and pagination with total count | `criteria.From/Select/Join/Where`, `In`, `Lte`, `IsTrue`, `FindFromCriteria(...).WithPage(...).PagedList()` |
| `(name ILIKE … OR slug = …)` | `criteria.Group(..., criteria.Or(), ...)` |
| One row, `nil` when absent | `UniqueResult()` |
| Stream rows without building a slice | `All()` (`iter.Seq2`) |

- `database.New()` reads `DATABASE_*` and registers the global connection that
  every `sqln` call uses; `DATABASE_MIGRATION=true` applies `.migrations/`.
- Rows are mapped by the `db` struct tags, not by column position.
- A job has no `ListenAndServe`: `svc.Shutdown` closes the database and flushes the logs.

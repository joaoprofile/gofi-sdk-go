# sqln — dynamic filter API

An HTTP API where the **client chooses the filters**: the JSON body is turned
into SQL by `sqln`, through an allowlist (`sqln.FilterMapping`). Fields,
operators or sort columns outside the allowlist are rejected with `400`.

```
filter-api/
├── .env                 # DATABASE_* (read automatically outside prod/stage)
├── .migrations/         # schema + seed data, applied at startup
├── compose.yaml         # PostgreSQL
├── main.go              # gofi.New().With(database.New(), httpserver.New(...))
└── product/
    ├── handler.go       # GET /products/filters, POST /products/search
    └── repository.go    # allowlist + BuildQuery + FindWithFilter
```

## Run

```sh
cd examples/sqln/filter-api
docker compose up -d     # PostgreSQL on localhost:5432
go run .                 # API on :8080 — Ctrl+C to stop
docker compose down      # when you are done
```

## Try it

What can be filtered (the allowlist, without the SQL columns):

```sh
curl localhost:8080/products/filters
```

Electronics or books up to 400, most expensive first, 3 per page:

```sh
curl -X POST localhost:8080/products/search -H 'Content-Type: application/json' -d '{
  "params":  {"page": 0, "limit": 3, "sortField": "price", "sortDirection": "DESC"},
  "filters": [
    {"field": "category", "condition": "IN", "value": ["electronics", "books"]},
    {"logicalOperator": "AND"},
    {"field": "price", "condition": "<=", "value": 400}
  ]}'
```

Name contains "design" **or** price below 30:

```sh
curl -X POST localhost:8080/products/search -H 'Content-Type: application/json' -d '{
  "filters": [
    {"field": "name", "condition": "LIKE", "value": "design"},
    {"logicalOperator": "OR"},
    {"field": "price", "condition": "<", "value": 30}
  ]}'
```

Created between two dates (`start|end`, RFC 3339), oldest first:

```sh
curl -X POST localhost:8080/products/search -H 'Content-Type: application/json' -d '{
  "params":  {"sortField": "created"},
  "filters": [{"field": "created", "condition": "BETWEEN", "value": "2026-01-01T00:00:00Z|2026-02-28T23:59:59Z"}]}'
```

Rejected — `active` is not in the allowlist, `LIKE` is not allowed on `category`:

```sh
curl -X POST localhost:8080/products/search -H 'Content-Type: application/json' -d '{"filters": [{"field": "active", "condition": "=", "value": false}]}'
curl -X POST localhost:8080/products/search -H 'Content-Type: application/json' -d '{"filters": [{"field": "category", "condition": "LIKE", "value": "b"}]}'
# {"code":400,"message":"sqln/filter: invalid filter at 0: field \"active\" is not in the mapping"}
```

The response is a `sqln.Page`: `content`, `totalElements`, `totalPages`, `number`, `size`.

## How it works

```go
var Fields = sqln.FilterMapping{
    "name":     {Column: "p.name", Ops: sqln.Text, Sortable: true},
    "category": {Column: "c.slug", Ops: sqln.Equality},
    "price":    {Column: "p.price", Ops: sqln.Range, Sortable: true},
    "created":  {Column: "p.created_at", Ops: sqln.Range, Sortable: true},
}

q, err := sqln.BuildQuery(baseQuery, nil, filters, Fields, nil) // base ... WHERE p.active IS TRUE AND ( filters )
page, err := sqln.NewPageRequestFilter(filters, Fields)        // page, limit and sort from "params"
return sqln.FindWithFilter[Product](ctx, q).WithPage(page).PagedList()
```

- Clients use API names (`price`), never column names (`p.price`); values are always bound as parameters.
- `Ops` limits the operators per field: `sqln.Text`, `sqln.Equality`, `sqln.Range`.
- Operators: `=` `!=` `<` `<=` `>` `>=` `IN` `NOT IN` `LIKE` `NOT LIKE` `BETWEEN` `IS NULL` `IS NOT NULL`;
  filters are joined by `{"logicalOperator": "AND"}` or `"OR"`.

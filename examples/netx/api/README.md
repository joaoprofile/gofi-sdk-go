# netx — API example

A minimal HTTP API built with `netx`: several handlers, public and private
routes, a global middleware and an auth middleware, all wired in `main.go`
through the gofi `httpserver` component.

```
api/
├── .env                    # APP_ENVIRONMENT (read automatically outside prod/stage)
├── main.go                 # gofi.New().With(httpserver.New(...)): middlewares + handlers
├── handler/
│   ├── health.go           # public            GET
│   ├── product.go          # public reads,     GET POST PUT PATCH DELETE
│   │                       # private writes
│   └── order.go            # private           GET POST
└── middleware/
    ├── api_version.go      # global  → server.Use
    └── auth.go             # private → server.UseAuth
```

## Run

Its own Go module: no database or broker needed.

```sh
cd examples/netx/api
go run .
```

Private routes require `Authorization: Bearer secret-token`.

```sh
TOKEN='Authorization: Bearer secret-token'
JSON='Content-Type: application/json'   # netx rejects other bodies with 415

# public
curl localhost:8080/health
curl 'localhost:8080/products?name=pen&min_stock=1'
curl localhost:8080/products/1

# private
curl -X POST   localhost:8080/products   -H "$TOKEN" -H "$JSON" -d '{"name":"Pen","price":2.5,"stock":10}'
curl -X PUT    localhost:8080/products/1 -H "$TOKEN" -H "$JSON" -d '{"name":"Blue Pen","price":3,"stock":7}'
curl -X PATCH  localhost:8080/products/1 -H "$TOKEN" -H "$JSON" -d '{"stock":2}'
curl -X DELETE localhost:8080/products/1 -H "$TOKEN"
curl -X POST   localhost:8080/orders     -H "$TOKEN" -H "$JSON" -d '{"product_id":1,"quantity":2}'
curl 'localhost:8080/orders?status=pending' -H "$TOKEN"
```

## API used

| Area | API |
|------|-----|
| Service | `gofi.New`, `With`, `Build`, `ListenAndServe` |
| Server | `httpserver.New` with `netx.WSConfig`, `Use`, `UseAuth`, `Handlers` |
| Routes | `GET` `POST` `PUT` `PATCH` `DELETE`, `To`, `Cors`, `Timeouts`, `PublicRoutes`, `PrivateRoutes`, `RouterHandler` |
| Request | `ParseRequestBody`, `GetPathParam`, `GetQueryParam`, `BindQueryParamsToStruct`, `GetRequestID` |
| Response | `Response`, `JSON`, `Error`, `ErrorDetails`, `RespondError` (with `base/errs`) |

`Build` sets up the logger `netx` writes to, and `ListenAndServe` blocks
until SIGINT/SIGTERM, then drains the server gracefully.

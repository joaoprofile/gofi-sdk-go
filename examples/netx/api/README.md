# netx — API example

A minimal HTTP API built with `netx`: several handlers, public and private
routes, a global middleware and an auth middleware, all wired in `main.go`.

```
api/
├── main.go                 # logger + server + middlewares + handlers
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

```sh
go run ./netx/api    # from examples/
```

Private routes require `Authorization: Bearer secret-token`.

```sh
TOKEN='Authorization: Bearer secret-token'

# public
curl localhost:8080/health
curl 'localhost:8080/products?name=pen&min_stock=1'
curl localhost:8080/products/1

# private
curl -X POST   localhost:8080/products   -H "$TOKEN" -d '{"name":"Pen","price":2.5,"stock":10}'
curl -X PUT    localhost:8080/products/1 -H "$TOKEN" -d '{"name":"Blue Pen","price":3,"stock":7}'
curl -X PATCH  localhost:8080/products/1 -H "$TOKEN" -d '{"stock":2}'
curl -X DELETE localhost:8080/products/1 -H "$TOKEN"
curl -X POST   localhost:8080/orders     -H "$TOKEN" -d '{"product_id":1,"quantity":2}'
curl 'localhost:8080/orders?status=pending' -H "$TOKEN"
```

## netx API used

| Area | API |
|------|-----|
| Server | `NewServer`, `WSConfig`, `Use`, `UseAuth`, `AddHandlers`, `ListenAndServe` |
| Routes | `GET` `POST` `PUT` `PATCH` `DELETE`, `To`, `Cors`, `Timeouts`, `PublicRoutes`, `PrivateRoutes`, `RouterHandler` |
| Request | `ParseRequestBody`, `GetPathParam`, `GetQueryParam`, `BindQueryParamsToStruct`, `GetRequestID` |
| Response | `Response`, `JSON`, `Error`, `ErrorDetails`, `RespondError` (with `base/errs`) |

> `netx` logs through `obs/logging`: call `logging.InitGlobal` before serving,
> otherwise the logging middleware panics on every request.

// Command api is a minimal HTTP API built with the netx package: handlers,
// public and private routes, global and auth middlewares, all wired here.
package main

import (
	"log"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/examples/netx/api/handler"
	"github.com/joaoprofile/gofi-sdk-go/examples/netx/api/middleware"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/httpserver"
	"github.com/joaoprofile/gofi-sdk-go/netx"
)

func main() {
	// Build loads .env, sets up logging (APP_ENVIRONMENT, LOG_LEVEL) and
	// starts the components.
	svc, err := gofi.New("example-api").
		With(
			// 1. Server config. Every field is optional; zero values fall back to defaults.
			httpserver.New(":8080", &netx.WSConfig{
				AllowedOrigins: []string{"http://localhost:3000"},
				MaxBodyBytes:   1 << 20, // 1 MB
				RequestTimeout: 10 * time.Second,
			}).
				// 2. Global middlewares: run on every route.
				Use(middleware.APIVersion("v1")).
				// 3. Auth middleware: runs only on routes declared with netx.PrivateRoutes.
				UseAuth(middleware.Auth).
				// 4. Handlers: each one declares its own routes.
				Handlers(
					handler.NewHealthHandler(),
					handler.NewProductHandler(),
					handler.NewOrderHandler(),
				),
		).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// 5. Blocks until SIGINT/SIGTERM, then shuts down gracefully.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

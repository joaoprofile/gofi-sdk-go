// Command api is a minimal HTTP API built with the netx package: handlers,
// public and private routes, global and auth middlewares, all wired here.
package main

import (
	"context"
	"log"
	"time"

	"github.com/joaoprofile/gofi/examples/netx/api/handler"
	"github.com/joaoprofile/gofi/examples/netx/api/middleware"
	"github.com/joaoprofile/gofi/netx"
	"github.com/joaoprofile/gofi/obs/logging"
)

func main() {
	// 0. netx logs through obs/logging, so the global logger must exist first.
	if err := logging.InitGlobal(context.Background(), logging.Config{
		ServiceName: "example-api",
		Environment: logging.EnvDevelopment,
	}); err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logging.Shutdown(context.Background())

	// 1. Server config. Every field is optional; zero values fall back to defaults.
	server := netx.NewServer(&netx.WSConfig{
		ServerPort:     ":8080",
		AllowedOrigins: []string{"http://localhost:3000"},
		MaxBodyBytes:   1 << 20, // 1 MB
		RequestTimeout: 10 * time.Second,
	})

	// 2. Global middlewares: run on every route.
	server.Use(middleware.APIVersion("v1"))

	// 3. Auth middleware: runs only on routes declared with netx.PrivateRoutes.
	server.UseAuth(middleware.Auth)

	// 4. Handlers: each one declares its own routes.
	server.AddHandlers(
		handler.NewHealthHandler(),
		handler.NewProductHandler(),
		handler.NewOrderHandler(),
	)

	// 5. Blocks until SIGINT/SIGTERM, then shuts down gracefully.
	server.ListenAndServe()
}

// Command filter-api is an HTTP API whose search filters are chosen by the
// client and translated to SQL by sqln, through an allowlist.
package main

import (
	"log"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/database"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/httpserver"
	"github.com/gofi-labs/gofi-sdk-go/netx"
	_ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/postgres" // DATABASE_DRIVER=postgres

	"github.com/gofi-labs/gofi-sdk-go/examples/sqln/filter-api/product"
)

func main() {
	svc, err := gofi.New("product-filter-api").
		With(
			database.New(), // DATABASE_* → global sqln connection; DATABASE_MIGRATION=true applies .migrations
			httpserver.New(":8080").
				Use(netx.LoggingMiddleware()).
				Handlers(product.NewHandler(product.NewRepository())),
		).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// Blocks until SIGINT/SIGTERM, then drains HTTP and closes the database.
	if err := svc.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

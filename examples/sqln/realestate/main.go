// Command realestate is a property catalog searched like a web search
// engine: one text box, where "casa nova de 2 quartos, suíte, garagem 2
// carros, curitiba, pr, bairro batel" becomes filters plus a full-text search.
package main

import (
	"context"
	"log"

	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/database"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/httpserver"
	"github.com/joaoprofile/gofi-sdk-go/netx/httpx"
	_ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres" // DATABASE_DRIVER=postgres

	"github.com/joaoprofile/gofi-sdk-go/examples/sqln/realestate/property"
	"github.com/joaoprofile/gofi-sdk-go/examples/sqln/realestate/web"
)

func main() {
	svc := property.NewService()
	app, err := gofi.New("realestate").
		With(
			// DATABASE_* → global sqln connection; DATABASE_MIGRATION=true applies
			// .migrations, then the full-text index is checked.
			database.New(),
			httpserver.New(":8080").
				Use(httpx.LoggingMiddleware()).
				Handlers(property.NewHandler(svc), web.Handler{}),
		).
		Build()
	if err != nil {
		log.Fatal(err)
	}
	// Reference data and the lexicon of "você quis dizer".
	if err := svc.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Print("http://localhost:8080")
	if err := app.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

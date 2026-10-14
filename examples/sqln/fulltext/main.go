// Command fulltext is a job (no HTTP server): it opens PostgreSQL through
// gofi, applies the migrations (the catalog and its full-text index), runs a
// few full-text searches, prints the results and exits.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/database"
	_ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres" // DATABASE_DRIVER=postgres
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"

	"github.com/joaoprofile/gofi-sdk-go/examples/sqln/fulltext/product"
)

func main() {
	// DATABASE_* → global sqln connection; DATABASE_MIGRATION=true applies .migrations.
	svc, err := gofi.New("product-fulltext").
		With(database.New()).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	err = run(context.Background(), product.NewRepository())

	if shErr := svc.Shutdown(context.Background()); shErr != nil {
		log.Print(shErr)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, repo *product.Repository) error {
	searches := []struct {
		title, term string
		mode        fts.Mode
	}{
		{"Words, without accents: \"cafe eletrica\"", "cafe eletrica", fts.Websearch},
		{"Ranked: \"cafe\" (in the name ranks above the description)", "cafe", fts.Websearch},
		{"Phrase and exclusion: `\"cafe coado\" -moedor`", `"cafe coado" -moedor`, fts.Websearch},
		{"Alternatives: \"chaleira OR garrafa\"", "chaleira OR garrafa", fts.Websearch},
		{"Search as you type: \"note ultra\"", "note ultra", fts.Prefix},
		{"Only stopwords: \"de para\" (empty page, not an error)", "de para", fts.Websearch},
	}
	for _, s := range searches {
		fmt.Printf("== %s ==\n", s.title)
		page, err := repo.Find(ctx, s.term, s.mode, 0, 5)
		if err != nil {
			return fmt.Errorf("search %q: %w", s.term, err)
		}
		for _, h := range page.Content {
			fmt.Printf("%3d  %-28s %.4f\n", h.ID, h.Name, h.Relevance)
		}
		fmt.Printf("-- %d found\n\n", page.TotalElements)
	}
	return nil
}

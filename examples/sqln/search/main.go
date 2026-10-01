// Command search is a job (no HTTP server): it opens PostgreSQL through gofi,
// applies the migrations, runs a few criteria queries, prints the results and exits.
package main

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"

	"github.com/gofi-labs/gofi-sdk-go/gofi"
	"github.com/gofi-labs/gofi-sdk-go/gofi/component/database"
	"github.com/gofi-labs/gofi-sdk-go/sqln"
	_ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/postgres" // DATABASE_DRIVER=postgres

	"github.com/gofi-labs/gofi-sdk-go/examples/sqln/search/product"
)

func main() {
	// DATABASE_* → global sqln connection; DATABASE_MIGRATION=true applies .migrations.
	svc, err := gofi.New("product-search").
		With(database.New()).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	err = run(context.Background(), product.NewRepository())

	// A job has no ListenAndServe: Shutdown closes the database and flushes the logs.
	if shErr := svc.Shutdown(context.Background()); shErr != nil {
		log.Print(shErr)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, repo *product.Repository) error {
	// 1. Filters + pagination: walk every page, 3 products per page, cheapest first.
	fmt.Println("== Electronics and books up to 400, cheapest first ==")
	filter := product.Filter{Categories: []string{"electronics", "books"}, MaxPrice: 400}
	for n := uint16(0); ; n++ {
		page, err := repo.Search(ctx, filter, sqln.NewPageRequest(n, 3, []sqln.Sort{sqln.NewSort("p.price", sqln.ASC)}))
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		fmt.Printf("-- page %d of %d (%d products in total)\n", page.Number+1, page.TotalPages, page.TotalElements)
		printAll(page.Content)
		if page.Number+1 >= page.TotalPages {
			break
		}
	}

	// 2. OR group: the term matches the name or the category.
	fmt.Println("\n== Term \"office\": name contains it or category is it ==")
	page, err := repo.Search(ctx, product.Filter{Term: "office"}, sqln.NewPageRequest(0, 10, nil))
	if err != nil {
		return fmt.Errorf("search term: %w", err)
	}
	printAll(page.Content)

	// 3. Single row: nil when not found.
	fmt.Println("\n== Product 7 ==")
	p, err := repo.FindByID(ctx, 7)
	if err != nil {
		return fmt.Errorf("find by id: %w", err)
	}
	if p != nil {
		printAll([]product.Product{*p})
	}

	// 4. Streaming: aggregate in Go while rows are read.
	fmt.Println("\n== Catalog value per category (streamed) ==")
	totals := map[string]float64{}
	for p, err := range repo.All(ctx) {
		if err != nil {
			return fmt.Errorf("stream: %w", err)
		}
		totals[p.Category] += p.Price
	}
	for _, category := range slices.Sorted(maps.Keys(totals)) {
		fmt.Printf("%-12s %10.2f\n", category, totals[category])
	}
	return nil
}

func printAll(products []product.Product) {
	for _, p := range products {
		fmt.Printf("%3d  %-40s %-12s %10.2f\n", p.ID, p.Name, p.Category, p.Price)
	}
}

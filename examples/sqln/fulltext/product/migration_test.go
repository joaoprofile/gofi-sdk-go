package product

import (
	"flag"
	"os"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

var update = flag.Bool("update", false, "rewrite the full-text migration from searchIndex")

// TestMigrationIsCurrent fails when searchIndex changed and its migration was not
// regenerated (go generate ./...): the build catches the drift before the
// connection would refuse the search with fts.ErrIndexOutdated.
func TestMigrationIsCurrent(t *testing.T) {
	const dir, version, name = "../.migrations", 2, "products_search"
	if *update {
		if err := fts.WriteMigration(dir, version, name, postgres.PostgresDialect{}, searchIndex); err != nil {
			t.Fatal(err)
		}
	}
	up, down, err := fts.Migration(postgres.PostgresDialect{}, searchIndex)
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		"../.migrations/000002_products_search.up.sql":   up,
		"../.migrations/000002_products_search.down.sql": down,
	} {
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%v: run go generate ./...", err)
		}
		if string(got) != want {
			t.Errorf("%s is out of date: run go generate ./...", file)
		}
	}
}

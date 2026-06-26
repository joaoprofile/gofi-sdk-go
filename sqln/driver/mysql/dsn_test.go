package mysql

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 3306, User: "user", Password: "pass", Name: "mydb",
	})
	want := "user:pass@tcp(localhost:3306)/mydb"
	if got != want {
		t.Errorf("DSN=%q, want %q", got, want)
	}
}

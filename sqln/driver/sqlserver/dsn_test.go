package sqlserver

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "host", Port: 1433, User: "u", Password: "p", Name: "db",
	})
	want := "sqlserver://u:p@host:1433?database=db"
	if got != want {
		t.Errorf("DSN=%q, want %q", got, want)
	}
}

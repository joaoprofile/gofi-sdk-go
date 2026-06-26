package oracle

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "host", Port: 1521, User: "u", Password: "p", Name: "service",
	})
	want := "oracle://u:p@host:1521/service"
	if got != want {
		t.Errorf("DSN=%q, want %q", got, want)
	}
}

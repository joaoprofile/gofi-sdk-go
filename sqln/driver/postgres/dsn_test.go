package postgres

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/connection"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 5432, User: "user", Password: "pass",
		Name: "mydb", SSLMode: "disable",
	})
	want := "host=localhost port=5432 user=user password=pass dbname=mydb sslmode=disable"
	if got != want {
		t.Errorf("DSN=%q, want %q", got, want)
	}
}

func TestDSN_DefaultsSSLModeToDisable(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{Host: "h", Port: 5432, Name: "d"})
	want := "host=h port=5432 user= password= dbname=d sslmode=disable"
	if got != want {
		t.Errorf("DSN=%q, want %q", got, want)
	}
}

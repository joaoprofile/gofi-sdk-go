package config

import (
	"strings"
	"testing"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/sqln/connection"
	_ "github.com/joaoprofile/gofi/sqln/driver/postgres"
)

func TestDatabase_Postgres(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_HOST", "localhost")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_USER", "user")
	t.Setenv("DATABASE_PASSWORD", "pass")
	t.Setenv("DATABASE_NAME", "mydb")
	t.Setenv("DATABASE_SSL_MODE", "disable")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "20")

	cfg, err := Database(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Driver != connection.DriverPostgres {
		t.Errorf("Driver=%q, want postgres", cfg.Driver)
	}
	want := "host=localhost port=5432 user=user password=pass dbname=mydb sslmode=disable"
	if cfg.DSN != want {
		t.Errorf("DSN=%q, want %q", cfg.DSN, want)
	}
	if cfg.Pool.MaxOpenConns != 20 {
		t.Errorf("MaxOpenConns=%d, want 20", cfg.Pool.MaxOpenConns)
	}
}

func TestDatabase_DefaultsToPostgres(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("DATABASE_HOST", "h")
	t.Setenv("DATABASE_NAME", "d")

	cfg, err := Database(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Driver != connection.DriverPostgres {
		t.Errorf("Driver=%q, want postgres (default)", cfg.Driver)
	}
	if !strings.HasPrefix(cfg.DSN, "host=h ") {
		t.Errorf("DSN=%q, expected postgres key-value form", cfg.DSN)
	}
}

func TestDatabase_UnregisteredDriver(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "totally-bogus-db")

	if _, err := Database(environment.Instance()); err == nil {
		t.Fatal("expected error for unregistered driver")
	}
}

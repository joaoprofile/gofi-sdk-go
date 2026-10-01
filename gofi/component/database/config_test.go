package database

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	_ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
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

	cfg, err := ConfigFromEnv(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Driver != connection.DriverPostgres {
		t.Errorf("Driver=%q, want postgres", cfg.Driver)
	}
	want := "host='localhost' port='5432' user='user' password='pass' dbname='mydb' sslmode='disable'"
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

	cfg, err := ConfigFromEnv(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Driver != connection.DriverPostgres {
		t.Errorf("Driver=%q, want postgres (default)", cfg.Driver)
	}
	// Empty DATABASE_SSL_MODE on a remote host means verify-full.
	if cfg.DSN != "host='h' dbname='d' sslmode='verify-full'" {
		t.Errorf("DSN=%q, expected postgres key-value form", cfg.DSN)
	}
}

func TestDatabase_TLSAndTimeouts(t *testing.T) {
	cfg, err := ConfigFromEnv(&environment.Environment{
		DatabaseHost: "db", DatabaseName: "d", DatabaseSSLMode: "verify-ca",
		DatabaseSSLRootCert: "/ca.pem", DatabaseSSLCert: "/c.pem", DatabaseSSLKey: "/k.pem",
		DatabaseStatementTimeout: 5 * time.Second, DatabaseQueryTimeout: 10 * time.Second,
		DatabaseReadHost: "replica",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "sslmode='verify-ca' sslrootcert='/ca.pem' sslcert='/c.pem' sslkey='/k.pem' statement_timeout='5000'"
	if !strings.HasSuffix(cfg.DSN, want) || !strings.HasSuffix(cfg.ReadDSN, want) {
		t.Errorf("DSN=%q ReadDSN=%q, want suffix %q", cfg.DSN, cfg.ReadDSN, want)
	}
	if cfg.QueryTimeout != 10*time.Second {
		t.Errorf("QueryTimeout=%v", cfg.QueryTimeout)
	}
}

func TestDatabase_InvalidSettingsFail(t *testing.T) {
	if _, err := ConfigFromEnv(&environment.Environment{DatabaseHost: "db", DatabaseSSLMode: "bogus"}); !errors.Is(err, connection.ErrInvalidSettings) {
		t.Errorf("invalid primary: %v", err)
	}
	_, err := ConfigFromEnv(&environment.Environment{DatabaseHost: "db", DatabaseReadHost: "bad host"})
	if !errors.Is(err, connection.ErrInvalidSettings) || !strings.Contains(err.Error(), "DATABASE_READ_") {
		t.Errorf("invalid replica: %v", err)
	}
}

func TestDatabase_InsecureSettings(t *testing.T) {
	cases := []struct {
		env  environment.Environment
		want int
	}{
		{environment.Environment{DatabaseHost: "localhost", DatabaseSSLMode: "disable"}, 0},
		{environment.Environment{DatabaseHost: "db"}, 0}, // verify-full by default
		{environment.Environment{DatabaseHost: "db", DatabaseSSLMode: "require"}, 1},
		{environment.Environment{DatabaseHost: "db", DatabaseSSLMode: "disable", DatabaseReadHost: "replica"}, 2},
		{environment.Environment{DatabaseHost: "127.0.0.1", DatabaseSSLMode: "prefer", DatabaseReadHost: "replica"}, 1},
	}
	for _, c := range cases {
		if got := insecureSettings(&c.env); len(got) != c.want {
			t.Errorf("%s/%s ssl=%q: got %d, want %d", c.env.DatabaseHost, c.env.DatabaseReadHost, c.env.DatabaseSSLMode, len(got), c.want)
		}
	}
}

func TestDatabase_UnregisteredDriver(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "totally-bogus-db")

	if _, err := ConfigFromEnv(environment.Instance()); err == nil {
		t.Fatal("expected error for unregistered driver")
	}
}

func TestDatabase_PoolOverrides(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_NAME", "d")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "30")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "7")
	t.Setenv("DATABASE_MAX_LIFETIME", "90s")

	cfg, err := ConfigFromEnv(environment.Instance())
	if err != nil {
		t.Fatalf("Database error: %v", err)
	}
	if cfg.Pool.MaxOpenConns != 30 || cfg.Pool.MaxIdleConns != 7 || cfg.Pool.MaxConnLifeTime != 90*time.Second {
		t.Errorf("pool overrides not applied: %+v", cfg.Pool)
	}
}

func TestDatabase_ReplicaAndIdleTime(t *testing.T) {
	environment.ResetForTesting()
	t.Cleanup(environment.ResetForTesting)
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_HOST", "primary")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_READ_HOST", "replica")
	t.Setenv("DATABASE_MAX_IDLE_TIME", "45s")

	cfg, err := ConfigFromEnv(environment.Instance())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.ReadDSN, "host='replica' port='5432' ") || !strings.HasPrefix(cfg.DSN, "host='primary' port='5432' ") {
		t.Errorf("DSN=%q ReadDSN=%q", cfg.DSN, cfg.ReadDSN)
	}
	if cfg.Pool.MaxIdleTime != 45*time.Second {
		t.Errorf("MaxIdleTime=%v", cfg.Pool.MaxIdleTime)
	}
}

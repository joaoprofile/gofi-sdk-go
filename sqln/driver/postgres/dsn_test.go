package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 5432, User: "user", Password: "pass",
		Name: "mydb", SSLMode: "disable",
	})
	want := "host='localhost' port='5432' user='user' password='pass' dbname='mydb' sslmode='disable'"
	assert.Equal(t, want, got)
}

func TestDSN_SSLModeDefaults(t *testing.T) {
	cases := map[string]string{
		"localhost":           "disable",
		"127.0.0.1":           "disable",
		"::1":                 "disable",
		"/var/run/postgresql": "disable",
		"db.example.com":      "verify-full",
		"10.0.0.5":            "verify-full",
	}
	for host, mode := range cases {
		cc, err := pgx.ParseConfig(Driver{}.DSN(connection.Settings{Host: host, Name: "d"}))
		require.NoError(t, err, host)
		if mode == "disable" {
			assert.Nil(t, cc.TLSConfig, host)
		} else {
			require.NotNil(t, cc.TLSConfig, host)
			assert.False(t, cc.TLSConfig.InsecureSkipVerify, host)
			assert.Equal(t, host, cc.TLSConfig.ServerName, host)
		}
	}
}

// Regression: an unquoted password with a space added keys to the DSN and
// redirected the connection to another host.
func TestDSN_HostileValuesStayValues(t *testing.T) {
	s := connection.Settings{
		Host:     "db.internal",
		Port:     5432,
		User:     `u' host=evil.example`,
		Password: `s3cr3t host=evil.example sslmode=disable \' '`,
		Name:     "app",
		SSLMode:  "verify-full",
	}
	cc, err := pgx.ParseConfig(Driver{}.DSN(s))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", cc.Host)
	assert.Equal(t, s.Password, cc.Password)
	assert.Equal(t, s.User, cc.User)
	assert.Equal(t, "app", cc.Database)
	require.NotNil(t, cc.TLSConfig, "sslmode must not be overridden")
	assert.Empty(t, cc.Fallbacks)
}

func TestDSN_TLSFilesAndStatementTimeout(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "db", SSLMode: "verify-ca", SSLRootCert: "/ca.pem", SSLCert: "/c.pem", SSLKey: "/k.pem",
		StatementTimeout: 5 * time.Second,
	})
	assert.Contains(t, got, "sslrootcert='/ca.pem'")
	assert.Contains(t, got, "sslcert='/c.pem' sslkey='/k.pem'")
	assert.Contains(t, got, "statement_timeout='5000'")
}

func TestBuildDSN_RejectsInvalidSettings(t *testing.T) {
	_, err := connection.BuildDSN(Driver{}, connection.Settings{Host: "db", Name: "a b"})
	assert.ErrorIs(t, err, connection.ErrInvalidSettings)
	_, err = connection.BuildDSN(Driver{}, connection.Settings{Host: "db", Name: "d", SSLMode: "bogus"})
	assert.ErrorIs(t, err, connection.ErrInvalidSettings)
	dsn, err := connection.BuildDSN(Driver{}, connection.Settings{Host: "/tmp", Name: "d"})
	require.NoError(t, err)
	assert.Contains(t, dsn, "host='/tmp'")
}

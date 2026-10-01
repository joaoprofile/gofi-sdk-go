package sqlserver

import (
	"net/url"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 1433, User: "u", Password: "p", Name: "db",
	})
	assert.Equal(t, "sqlserver://u:p@localhost:1433?database=db&encrypt=disable", got)
}

func TestDSN_TLSModes(t *testing.T) {
	cases := map[string][2]string{
		"":            {"true", "false"},
		"verify-full": {"true", "false"},
		"verify-ca":   {"true", "false"}, // unsupported: the strictest mode
		"require":     {"true", "true"},
		"disable":     {"disable", ""},
	}
	for mode, want := range cases {
		q := parse(t, Driver{}.DSN(connection.Settings{Host: "db.example.com", SSLMode: mode})).Query()
		assert.Equal(t, want[0], q.Get("encrypt"), mode)
		assert.Equal(t, want[1], q.Get("TrustServerCertificate"), mode)
	}
	q := parse(t, Driver{}.DSN(connection.Settings{Host: "db", SSLRootCert: "/ca.pem"})).Query()
	assert.Equal(t, "/ca.pem", q.Get("certificate"))
}

// Regression: "db&encrypt=disable" as the name turned encryption off, and
// passwords with @:/# broke the URL.
func TestDSN_HostileValuesStayValues(t *testing.T) {
	s := connection.Settings{
		Host: "db.internal", Port: 1433, User: "app@x",
		Password: "p@ss:w/o?rd#&encrypt=disable",
		Name:     "db&encrypt=disable",
	}
	u := parse(t, Driver{}.DSN(s))
	pw, _ := u.User.Password()
	assert.Equal(t, s.Password, pw)
	assert.Equal(t, s.User, u.User.Username())
	assert.Equal(t, "db.internal:1433", u.Host)
	assert.Equal(t, url.Values{
		"database": {s.Name}, "encrypt": {"true"}, "TrustServerCertificate": {"false"},
	}, u.Query())

	_, err := connection.BuildDSN(Driver{}, s)
	assert.ErrorIs(t, err, connection.ErrInvalidSettings)
}

func TestDSN_IPv6(t *testing.T) {
	assert.Equal(t, "[::1]:1433", parse(t, Driver{}.DSN(connection.Settings{Host: "::1", Port: 1433})).Host)
	assert.Equal(t, "[::1]", parse(t, Driver{}.DSN(connection.Settings{Host: "::1"})).Host)
}

func TestValidateSettings(t *testing.T) {
	require.NoError(t, Driver{}.ValidateSettings(connection.Settings{Host: "db", SSLMode: "require"}))
	for name, s := range map[string]connection.Settings{
		"verify-ca":   {Host: "db", SSLMode: "verify-ca"},
		"prefer":      {Host: "db", SSLMode: "prefer"},
		"client cert": {Host: "db", SSLCert: "/c.pem", SSLKey: "/k.pem"},
		"timeout":     {Host: "db", StatementTimeout: 1},
	} {
		assert.ErrorIs(t, Driver{}.ValidateSettings(s), connection.ErrInvalidSettings, name)
	}
}

func parse(t *testing.T, dsn string) *url.URL {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	return u
}

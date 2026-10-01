package oracle

import (
	"net/url"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 1521, User: "u", Password: "p", Name: "service",
	})
	assert.Equal(t, "oracle://u:p@localhost:1521/service", got)
}

func TestDSN_TLSModes(t *testing.T) {
	cases := map[string][2]string{
		"":            {"true", "true"},
		"verify-full": {"true", "true"},
		"verify-ca":   {"true", "true"}, // unsupported: the strictest mode
		"require":     {"true", "false"},
		"disable":     {"", ""},
	}
	for mode, want := range cases {
		q := parse(t, Driver{}.DSN(connection.Settings{Host: "db.example.com", Name: "s", SSLMode: mode})).Query()
		assert.Equal(t, want[0], q.Get("SSL"), mode)
		assert.Equal(t, want[1], q.Get("SSL VERIFY"), mode)
	}
}

// Regression: values were concatenated into the URL unescaped.
func TestDSN_HostileValuesStayValues(t *testing.T) {
	s := connection.Settings{
		Host: "db.internal", Port: 1521, User: "app",
		Password: "p@ss:w/o?rd#&SSL=false",
		Name:     "svc?SSL=false",
	}
	u := parse(t, Driver{}.DSN(s))
	pw, _ := u.User.Password()
	assert.Equal(t, s.Password, pw)
	assert.Equal(t, "db.internal:1521", u.Host)
	assert.Equal(t, "/"+s.Name, u.Path)
	assert.Equal(t, url.Values{"SSL": {"true"}, "SSL VERIFY": {"true"}}, u.Query())

	_, err := connection.BuildDSN(Driver{}, s)
	assert.ErrorIs(t, err, connection.ErrInvalidSettings)
}

func TestValidateSettings(t *testing.T) {
	require.NoError(t, Driver{}.ValidateSettings(connection.Settings{Host: "db"}))
	for name, s := range map[string]connection.Settings{
		"verify-ca": {Host: "db", SSLMode: "verify-ca"},
		"root cert": {Host: "db", SSLRootCert: "/ca.pem"},
		"timeout":   {Host: "db", StatementTimeout: 1},
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

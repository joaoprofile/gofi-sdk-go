package mysql

import (
	"net/url"
	"strings"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDSN(t *testing.T) {
	got := Driver{}.DSN(connection.Settings{
		Host: "localhost", Port: 3306, User: "user", Password: "pass", Name: "mydb",
	})
	assert.Equal(t, "user:pass@tcp(localhost:3306)/mydb?tls=false", got)
}

func TestDSN_TLSModes(t *testing.T) {
	cases := map[string]string{
		"":            "true", // non-local host: verify-full
		"disable":     "false",
		"prefer":      "preferred",
		"require":     "skip-verify",
		"verify-full": "true",
		"verify-ca":   "true", // unsupported: the strictest mode, never plaintext
	}
	for mode, want := range cases {
		p := parse(t, Driver{}.DSN(connection.Settings{Host: "db.example.com", Name: "d", SSLMode: mode}))
		assert.Equal(t, want, p.params.Get("tls"), mode)
	}
}

// Regression: the name was concatenated as is, so "db?allowAllFiles=true"
// enabled LOAD DATA LOCAL INFILE, and passwords with @:/ broke the DSN.
func TestDSN_HostileValuesStayValues(t *testing.T) {
	s := connection.Settings{
		Host: "db.internal", Port: 3306, User: "app",
		Password: "p@ss:w/o?rd#)&tls=false",
		Name:     "db?allowAllFiles=true&tls=false",
	}
	p := parse(t, Driver{}.DSN(s))
	assert.Equal(t, "app", p.user)
	assert.Equal(t, s.Password, p.password)
	assert.Equal(t, "tcp(db.internal:3306)", p.addr)
	assert.Equal(t, s.Name, p.dbname)
	assert.Equal(t, url.Values{"tls": {"true"}}, p.params)

	_, err := connection.BuildDSN(Driver{}, s)
	assert.ErrorIs(t, err, connection.ErrInvalidSettings, "such a name is rejected before it reaches the DSN")
}

func TestValidateSettings(t *testing.T) {
	ok := connection.Settings{Host: "db", Name: "d"}
	require.NoError(t, Driver{}.ValidateSettings(ok))
	for name, s := range map[string]connection.Settings{
		"verify-ca":  {Host: "db", SSLMode: "verify-ca"},
		"root cert":  {Host: "db", SSLRootCert: "/ca.pem"},
		"user colon": {Host: "db", User: "a:b"},
		"socket":     {Host: "/tmp/mysql.sock"},
	} {
		assert.ErrorIs(t, Driver{}.ValidateSettings(s), connection.ErrInvalidSettings, name)
	}
}

type parsedDSN struct {
	user, password, addr, dbname string
	params                       url.Values
}

// parse splits dsn with go-sql-driver/mysql's rules: the last '/' ends the
// address, the last '@' before it ends the credentials, the first ':' in
// them ends the user, and the name is path-unescaped.
func parse(t *testing.T, dsn string) parsedDSN {
	t.Helper()
	slash := strings.LastIndexByte(dsn, '/')
	require.Positive(t, slash)
	at := strings.LastIndexByte(dsn[:slash], '@')
	require.Positive(t, at)
	creds := dsn[:at]
	colon := strings.IndexByte(creds, ':')
	require.GreaterOrEqual(t, colon, 0)
	rest := dsn[slash+1:]
	name, rawParams, _ := strings.Cut(rest, "?")
	dbname, err := url.PathUnescape(name)
	require.NoError(t, err)
	params, err := url.ParseQuery(rawParams)
	require.NoError(t, err)
	return parsedDSN{user: creds[:colon], password: creds[colon+1:], addr: dsn[at+1 : slash], dbname: dbname, params: params}
}

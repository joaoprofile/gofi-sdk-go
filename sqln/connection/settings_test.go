package connection

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettings_EffectiveSSLMode(t *testing.T) {
	for host, want := range map[string]string{
		"":                    SSLDisable,
		"localhost":           SSLDisable,
		"LOCALHOST":           SSLDisable,
		"127.0.0.1":           SSLDisable,
		"127.1.2.3":           SSLDisable,
		"::1":                 SSLDisable,
		"[::1]":               SSLDisable,
		"/var/run/postgresql": SSLDisable,
		"db":                  SSLVerifyFull,
		"db.example.com":      SSLVerifyFull,
		"10.0.0.1":            SSLVerifyFull,
		"localhost.evil.com":  SSLVerifyFull,
	} {
		assert.Equal(t, want, Settings{Host: host}.EffectiveSSLMode(), host)
	}
	assert.Equal(t, SSLRequire, Settings{Host: "localhost", SSLMode: SSLRequire}.EffectiveSSLMode())
}

func TestSettings_Insecure(t *testing.T) {
	for mode, want := range map[string]bool{
		SSLDisable: true, SSLAllow: true, SSLPrefer: true, SSLRequire: true,
		SSLVerifyCA: false, SSLVerifyFull: false, "": false, "bogus": true,
	} {
		assert.Equal(t, want, Settings{Host: "db.example.com", SSLMode: mode}.Insecure(), mode)
	}
	s := Settings{Host: "localhost"}
	assert.True(t, s.Insecure())
	assert.True(t, s.IsLocal())
}

func TestSettings_Validate(t *testing.T) {
	require.NoError(t, Settings{Host: "db.example.com", Port: 5432, User: "u", Password: "p@ss w'rd\\", Name: "app_db-1.x$"}.Validate())
	require.NoError(t, Settings{Host: "/var/run/postgresql"}.Validate())
	require.NoError(t, Settings{Host: "::1"}.Validate())
	for name, s := range map[string]Settings{
		"port":         {Port: 70000},
		"host space":   {Host: "db host=evil"},
		"host query":   {Host: "db?x=1"},
		"host at":      {Host: "evil@db"},
		"host slash":   {Host: "db/x"},
		"name query":   {Name: "db?allowAllFiles=true"},
		"name amp":     {Name: "db&encrypt=disable"},
		"name hash":    {Name: "db#x"},
		"name space":   {Name: "db x"},
		"user control": {User: "u\nx"},
		"password nul": {Password: "a\x00b"},
		"mode":         {SSLMode: "on"},
		"cert no key":  {SSLCert: "/c.pem"},
		"timeout":      {StatementTimeout: -1},
	} {
		assert.ErrorIs(t, s.Validate(), ErrInvalidSettings, name)
	}
}

type modeDriver struct{ overwriteDriver }

func (modeDriver) ValidateSettings(s Settings) error { return s.CheckSSLMode(SSLVerifyFull) }
func (modeDriver) DSN(s Settings) string             { return "dsn:" + s.Host }

func TestBuildDSN(t *testing.T) {
	dsn, err := BuildDSN(modeDriver{}, Settings{Host: "db"})
	require.NoError(t, err)
	assert.Equal(t, "dsn:db", dsn)

	_, err = BuildDSN(modeDriver{}, Settings{Host: "db", SSLMode: SSLRequire})
	assert.ErrorIs(t, err, ErrInvalidSettings)
	_, err = BuildDSN(modeDriver{}, Settings{Host: "db x"})
	assert.ErrorIs(t, err, ErrInvalidSettings)
}

func TestWrapError_HidesMessageKeepsCause(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key", Detail: "Key (email)=(a@b.c) already exists."}
	err := WrapError("exec", pgErr)
	assert.Equal(t, "sqln: exec failed (SQLSTATE 23505)", err.Error())
	got, ok := AsPgError(err)
	require.True(t, ok)
	assert.Same(t, pgErr, got)
	assert.Same(t, pgErr, Cause(err))

	assert.Same(t, err, WrapError("query", err), "never wrapped twice")
	assert.Nil(t, WrapError("query", nil))
	assert.True(t, IsRetryable(WrapError("query", &pgconn.PgError{Code: "40001"})))

	assert.ErrorIs(t, WrapError("query", sql.ErrNoRows), sql.ErrNoRows)
	assert.Equal(t, "sqln: query failed", WrapError("query", errors.New("column \"secret\" does not exist")).Error())
	assert.Equal(t, "sqln: query failed: context deadline exceeded", WrapError("query", context.DeadlineExceeded).Error())
}

func TestWithTimeout(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, ok := ctx.Deadline()
	assert.True(t, ok)

	parent, pcancel := context.WithTimeout(context.Background(), time.Hour)
	defer pcancel()
	ctx, cancel = WithTimeout(parent, time.Second)
	defer cancel()
	assert.Equal(t, parent, ctx, "an existing deadline wins")

	ctx, cancel = WithTimeout(context.Background(), -1)
	defer cancel()
	_, ok = ctx.Deadline()
	assert.False(t, ok, "negative disables")
}

func TestQueryTimeout_Resolution(t *testing.T) {
	assert.Equal(t, DefaultQueryTimeout, resolveQueryTimeout(0))
	assert.Equal(t, time.Second, resolveQueryTimeout(time.Second))
	assert.Negative(t, resolveQueryTimeout(-1))
	var nilConn *Connection
	assert.Equal(t, DefaultQueryTimeout, nilConn.QueryTimeout())
	assert.Equal(t, DefaultQueryTimeout, NewRaw(nil, nil).QueryTimeout())
}

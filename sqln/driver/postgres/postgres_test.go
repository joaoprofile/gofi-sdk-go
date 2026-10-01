package postgres

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi-sdk-go/sqln/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriver_Name_ReturnsPostgres(t *testing.T) {
	assert.Equal(t, connection.DriverPostgres, Driver{}.Name())
}

func TestDriver_ParseError_NilInput_ReturnsNil(t *testing.T) {
	assert.Nil(t, Driver{}.ParseError(nil))
}

func TestDriver_ParseError_HidesMessageKeepsCause(t *testing.T) {
	err := errors.New("some db error")
	got := Driver{}.ParseError(err)
	assert.ErrorIs(t, got, err)
	assert.NotContains(t, got.Error(), "some db error")
}

func TestDriver_Dialect_ImplementsDialectInterface(t *testing.T) {
	var _ sqln_driver.Dialect = Driver{}.Dialect()
	require.NotNil(t, Driver{}.Dialect())
}

func TestDriver_Dialect_ReturnsPostgresDialect(t *testing.T) {
	_, ok := Driver{}.Dialect().(PostgresDialect)
	assert.True(t, ok)
}

func TestDriver_RegisteredInConnectionRegistry(t *testing.T) {
	d, ok := connection.GetDriver(connection.DriverPostgres)
	require.True(t, ok, "postgres driver should be auto-registered via init()")
	assert.Equal(t, connection.DriverPostgres, d.Name())
}

// Open — sql.Open is lazy: it validates driver registration without connecting.
// A real server is not required; errors only arise when the DB is actually used.
func TestDriver_Open_ReturnsSQLDB(t *testing.T) {
	db, err := Driver{}.Open(connection.Config{DSN: "host=127.0.0.1 port=1 dbname=test sslmode=disable"})
	require.NoError(t, err)
	require.NotNil(t, db)
	db.Close()
}

// Password runs before every new physical connection (IAM tokens).
func TestDriver_Open_PasswordFuncRunsPerConnection(t *testing.T) {
	var calls atomic.Int32
	db, err := Driver{}.Open(connection.Config{
		DSN: "host=127.0.0.1 port=1 dbname=test sslmode=verify-full connect_timeout=1",
		Password: func(context.Context) (string, error) {
			calls.Add(1)
			return "token", nil
		},
	})
	require.NoError(t, err)
	defer db.Close()
	_ = db.PingContext(context.Background()) // nothing listens on port 1
	assert.Positive(t, calls.Load())
}

// An IAM token must never reach a server whose certificate is not verified.
func TestDriver_Open_PasswordFuncRequiresVerifiedTLS(t *testing.T) {
	pw := func(context.Context) (string, error) { return "token", nil }
	for _, mode := range []string{"disable", "allow", "prefer", "require"} {
		_, err := Driver{}.Open(connection.Config{DSN: "host=db.example.com sslmode=" + mode, Password: pw})
		assert.ErrorIs(t, err, ErrPasswordFuncNeedsVerifiedTLS, mode)
	}
	for _, mode := range []string{"verify-ca", "verify-full"} {
		db, err := Driver{}.Open(connection.Config{DSN: "host=db.example.com sslmode=" + mode, Password: pw})
		require.NoError(t, err, mode)
		_ = db.Close()
	}
	// Without a password callback plaintext stays allowed (local development).
	db, err := Driver{}.Open(connection.Config{DSN: "host=127.0.0.1 sslmode=disable"})
	require.NoError(t, err)
	_ = db.Close()
}

func TestDriver_Open_InvalidDSN(t *testing.T) {
	_, err := Driver{}.Open(connection.Config{DSN: "port=notanumber"})
	assert.Error(t, err)
}

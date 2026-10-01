package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/lib/pq"
)

func TestNewUnregisteredDriverFailsStart(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{DatabaseDriver: "notregistered_driver_xyz"})
	c := New()
	err := c.Start(context.Background(), rt)
	assert.ErrorContains(t, err, `database driver "notregistered_driver_xyz" is not registered`)
	assert.Nil(t, c.DB())
	assert.Empty(t, rt.HealthChecks())
}

// Port 1 guarantees a fast connection-refused error from the default driver.
func TestNewConnectionErrorFailsStart(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{DatabaseHost: "localhost", DatabasePort: 1, DatabaseName: "d"})
	assert.Error(t, New().Start(context.Background(), rt))
}

func TestFromDBRegistersHealthCheckAndIsNotClosed(t *testing.T) {
	// sql.Open does not connect — safe to use in unit tests
	db, err := sql.Open("postgres", "host=localhost port=5432 dbname=test")
	require.NoError(t, err)
	rt := gofi.NewRuntime(&environment.Environment{})

	c := FromDB(db)
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Same(t, db, c.DB())
	assert.Same(t, db, c.ReadDB(), "without a replica ReadDB is the primary")
	require.Len(t, rt.HealthChecks(), 1)
	assert.Equal(t, "database", rt.HealthChecks()[0].Name)

	require.NoError(t, rt.Close(context.Background()))
	assert.NoError(t, db.Close(), "db must not have been closed by gofi")
}

func TestFromDBNilIsAccepted(t *testing.T) {
	rt := gofi.NewRuntime(&environment.Environment{})
	c := FromDB(nil)
	require.NoError(t, c.Start(context.Background(), rt))
	assert.Nil(t, c.DB())
	assert.Empty(t, rt.HealthChecks())
}

func TestNewOpensPrimaryAndReplicaAndClosesThem(t *testing.T) {
	registerFakeDriver()
	rt := gofi.NewRuntime(&environment.Environment{
		DatabaseDriver:   fakeDriverName,
		DatabaseHost:     "primary",
		DatabaseReadHost: "replica",
	})

	c := New()
	require.NoError(t, c.Start(context.Background(), rt))
	require.NotNil(t, c.DB())
	assert.NotSame(t, c.DB(), c.ReadDB(), "DATABASE_READ_HOST opens a replica pool")
	require.NoError(t, c.DB().PingContext(context.Background()))

	var names []string
	for _, hc := range rt.HealthChecks() {
		names = append(names, hc.Name)
		assert.NoError(t, hc.Check(context.Background()))
	}
	assert.Equal(t, []string{"database", "database-replica"}, names)

	require.NoError(t, rt.Close(context.Background()))
	assert.ErrorContains(t, c.DB().PingContext(context.Background()), "database is closed")
}

func TestInsecureTransports(t *testing.T) {
	var _ gofi.TransportChecker = New()
	env := &environment.Environment{AppEnvironment: "prod", DatabaseHost: "db", DatabaseSSLMode: "disable"}

	found := New().InsecureTransports(env)
	require.Len(t, found, 1)
	assert.Equal(t, gofi.InsecureTransport{Resource: "database", Setting: "DATABASE_SSL_MODE", Detail: "host db uses sslmode disable"}, found[0])
	assert.ErrorIs(t, core.CheckTransport(env, found), core.ErrInsecureTransport, "refused in prod")

	env.AllowInsecureTransport = "database"
	assert.NoError(t, core.CheckTransport(env, found), "allowed by the hatch")

	assert.Empty(t, FromDB(nil).InsecureTransports(env), "injected pools are the caller's")
	env.DatabaseSSLMode = "verify-full"
	assert.Empty(t, New().InsecureTransports(env))
}

func TestIdentity(t *testing.T) {
	assert.Equal(t, "database", New().Name())
	assert.Equal(t, gofi.StageDatabase, New().Stage())
}

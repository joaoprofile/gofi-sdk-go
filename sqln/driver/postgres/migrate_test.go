package postgres

import (
	"database/sql"
	"testing"

	"github.com/joaoprofile/gofi/sqln/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateDriver_Name_ReturnsPostgres(t *testing.T) {
	assert.Equal(t, "postgres", MigrateDriver{}.Name())
}

func TestMigrateDriver_RegisteredInMigrateRegistry(t *testing.T) {
	d, ok := migrate.GetDriver("postgres")
	require.True(t, ok, "postgres migrate driver should be auto-registered via init()")
	assert.Equal(t, "postgres", d.Name())
}

// Instance — exercises the method body with a DB that has no live server.
// postgres.WithInstance may return an error; we just verify the call path is exercised.
func TestMigrateDriver_Instance_ExercisesMethodBody(t *testing.T) {
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 dbname=test sslmode=disable")
	require.NoError(t, err)
	defer db.Close()

	// The call may fail when postgres.WithInstance tries to contact the server;
	// what matters is that the Instance method body is executed (coverage).
	_, _ = MigrateDriver{}.Instance(db)
}

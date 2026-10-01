package migrate

import (
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAndClose_ClosesInstance(t *testing.T) {
	skipUnderRace(t)
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	RegisterDriver(&fakeMigrateDriver{name: "driver-run-and-close", instanceDB: fakeDB})

	require.NoError(t, RunAndClose(&sql.DB{}, "driver-run-and-close", Config{Path: writeMigrationFiles(t)}))
	assert.True(t, fakeDB.closed, "the dedicated migration database must be released")
}

func TestRun_KeepsInstanceOpen(t *testing.T) {
	skipUnderRace(t)
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	RegisterDriver(&fakeMigrateDriver{name: "driver-run-keeps", instanceDB: fakeDB})

	require.NoError(t, Run(&sql.DB{}, "driver-run-keeps", Config{Path: writeMigrationFiles(t)}))
	assert.False(t, fakeDB.closed, "Run must not close the caller's pool")
}

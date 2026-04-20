package migrate

import (
	"database/sql"
	"embed"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embedded test migrations (used by runEmbedded tests)

//go:embed testdata/migrations
var testMigrationsFS embed.FS

// Fake Driver (migrate.Driver interface) for testing registry and Run

type fakeMigrateDriver struct {
	name        string
	instanceErr error
	instanceDB  database.Driver
}

func (d *fakeMigrateDriver) Name() string { return d.name }
func (d *fakeMigrateDriver) Instance(_ *sql.DB) (database.Driver, error) {
	return d.instanceDB, d.instanceErr
}

// Fake database.Driver (golang-migrate) for testing filesystem and embed

type fakeDatabaseDriver struct {
	version    int
	dirty      bool
	versionErr error
	runErr     error
}

func (f *fakeDatabaseDriver) Open(_ string) (database.Driver, error) { return f, nil }
func (f *fakeDatabaseDriver) Close() error                           { return nil }
func (f *fakeDatabaseDriver) Lock() error                            { return nil }
func (f *fakeDatabaseDriver) Unlock() error                          { return nil }
func (f *fakeDatabaseDriver) Drop() error                            { return nil }
func (f *fakeDatabaseDriver) SetVersion(version int, dirty bool) error {
	f.version = version
	f.dirty = dirty
	return nil
}
func (f *fakeDatabaseDriver) Version() (int, bool, error) {
	return f.version, f.dirty, f.versionErr
}
func (f *fakeDatabaseDriver) Run(_ io.Reader) error { return f.runErr }

// writeMigrationFiles creates 1_init.{up,down}.sql in a temp dir and returns the dir path.
func writeMigrationFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1_init.up.sql"), []byte("SELECT 1;"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1_init.down.sql"), []byte("SELECT 0;"), 0644))
	return dir
}

// registry.go — RegisterDriver / GetDriver / getDriver

func TestRegisterDriver_CanBeRetrieved(t *testing.T) {
	d := &fakeMigrateDriver{name: "test-migrate-driver"}
	RegisterDriver(d)

	got, ok := getDriver("test-migrate-driver")
	require.True(t, ok)
	assert.Equal(t, d, got)
}

func TestRegisterDriver_OverwritesExistingDriver(t *testing.T) {
	first := &fakeMigrateDriver{name: "overwrite-driver"}
	second := &fakeMigrateDriver{name: "overwrite-driver"}
	RegisterDriver(first)
	RegisterDriver(second)

	got, ok := getDriver("overwrite-driver")
	require.True(t, ok)
	assert.Equal(t, second, got, "second registration should overwrite the first")
}

func TestGetDriver_UnknownName_ReturnsFalse(t *testing.T) {
	_, ok := getDriver("this-driver-does-not-exist")
	assert.False(t, ok)
}

func TestGetDriver_PublicFunctionReturnsRegisteredDriver(t *testing.T) {
	d := &fakeMigrateDriver{name: "public-get-driver-test"}
	RegisterDriver(d)

	got, ok := GetDriver("public-get-driver-test")
	require.True(t, ok)
	assert.Equal(t, d, got)
}

func TestGetDriver_PublicFunction_UnknownName_ReturnsFalse(t *testing.T) {
	_, ok := GetDriver("public-unknown-driver-xyz")
	assert.False(t, ok)
}

// migrate.go — Run

func TestRun_UnregisteredDriver_ReturnsError(t *testing.T) {
	db := &sql.DB{}
	err := Run(db, "unregistered-driver-xyz", Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration driver not registered")
}

func TestRun_InstanceError_ReturnsError(t *testing.T) {
	instanceErr := errors.New("failed to create instance")
	d := &fakeMigrateDriver{name: "driver-instance-error", instanceErr: instanceErr}
	RegisterDriver(d)

	err := Run(&sql.DB{}, "driver-instance-error", Config{})
	require.Error(t, err)
	assert.ErrorIs(t, err, instanceErr)
}

func TestRun_WithRegisteredDriver_CallsRunFilesystem(t *testing.T) {
	// Run always delegates to runFilesystem because the FS self-comparison
	// (cfg.FS != cfg.FS) is always false. A missing migration directory is
	// enough to confirm the delegation path was reached.
	dir := writeMigrationFiles(t)
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	d := &fakeMigrateDriver{name: "driver-run-filesystem", instanceDB: fakeDB}
	RegisterDriver(d)

	cfg := Config{Path: dir}
	err := Run(&sql.DB{}, "driver-run-filesystem", cfg)
	require.NoError(t, err)
}

// filesystem.go — runFilesystem

func TestRunFilesystem_NonExistentPath_ReturnsError(t *testing.T) {
	cfg := Config{Path: "/tmp/nonexistent-migrate-test-xyz-999"}
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	err := runFilesystem("test-db", cfg, fakeDB)
	require.Error(t, err)
}

func TestRunFilesystem_Success(t *testing.T) {
	dir := writeMigrationFiles(t)
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	err := runFilesystem("test-db", Config{Path: dir}, fakeDB)
	require.NoError(t, err)
}

func TestRunFilesystem_NoChange_ReturnsNil(t *testing.T) {
	// DB already at the latest version: m.Up() returns ErrNoChange, which must be swallowed.
	dir := writeMigrationFiles(t)
	fakeDB := &fakeDatabaseDriver{version: 1}
	err := runFilesystem("test-db", Config{Path: dir}, fakeDB)
	require.NoError(t, err)
}

func TestRunFilesystem_VersionError_ReturnsError(t *testing.T) {
	dir := writeMigrationFiles(t)
	fakeDB := &fakeDatabaseDriver{
		version:    database.NilVersion,
		versionErr: errors.New("db version error"),
	}
	err := runFilesystem("test-db", Config{Path: dir}, fakeDB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration error")
}

func TestRunFilesystem_RunError_ReturnsError(t *testing.T) {
	dir := writeMigrationFiles(t)
	fakeDB := &fakeDatabaseDriver{
		version: database.NilVersion,
		runErr:  errors.New("sql execution error"),
	}
	err := runFilesystem("test-db", Config{Path: dir}, fakeDB)
	require.Error(t, err)
}

// embed.go — runEmbedded

func TestRunEmbedded_InvalidPath_ReturnsError(t *testing.T) {
	// iofs.New fails when the given path does not exist inside the FS.
	cfg := Config{FS: testMigrationsFS, Path: "nonexistent/path"}
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.Error(t, err)
}

func TestRunEmbedded_EmptyFS_ReturnsError(t *testing.T) {
	cfg := Config{FS: embed.FS{}, Path: "testdata/migrations"}
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.Error(t, err)
}

func TestRunEmbedded_Success(t *testing.T) {
	cfg := Config{FS: testMigrationsFS, Path: "testdata/migrations"}
	fakeDB := &fakeDatabaseDriver{version: database.NilVersion}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.NoError(t, err)
}

func TestRunEmbedded_NoChange_ReturnsNil(t *testing.T) {
	// DB already at the latest version: ErrNoChange must be swallowed.
	cfg := Config{FS: testMigrationsFS, Path: "testdata/migrations"}
	fakeDB := &fakeDatabaseDriver{version: 1}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.NoError(t, err)
}

func TestRunEmbedded_VersionError_ReturnsError(t *testing.T) {
	cfg := Config{FS: testMigrationsFS, Path: "testdata/migrations"}
	fakeDB := &fakeDatabaseDriver{
		version:    database.NilVersion,
		versionErr: errors.New("db version error"),
	}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration error")
}

func TestRunEmbedded_RunError_ReturnsError(t *testing.T) {
	cfg := Config{FS: testMigrationsFS, Path: "testdata/migrations"}
	fakeDB := &fakeDatabaseDriver{
		version: database.NilVersion,
		runErr:  errors.New("sql execution error"),
	}
	err := runEmbedded(nil, "test-db", cfg, fakeDB)
	require.Error(t, err)
}

package connection

import (
	"testing"

	"github.com/joaoprofile/gofi/sqln/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithMigrations_SetsConfig(t *testing.T) {
	migCfg := migrate.Config{Path: ".migrations"}
	opt := WithMigrations(migCfg)

	var opts options
	opt(&opts)

	require.NotNil(t, opts.migrationConfig)
	assert.Equal(t, ".migrations", opts.migrationConfig.Path)
}

func TestConfig_Fields(t *testing.T) {
	pool := DefaultPoolConfig()
	cfg := Config{
		Driver: DriverPostgres,
		DSN:    "postgres://localhost/test",
		Pool:   pool,
	}

	assert.Equal(t, DriverPostgres, cfg.Driver)
	assert.Equal(t, "postgres://localhost/test", cfg.DSN)
	assert.Equal(t, pool, cfg.Pool)
}

package config

import (
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/sqln/connection"
)

// Database builds a connection.Config from the DATABASE_* environment variables.
//
// It is driver-agnostic: the DSN is assembled by whichever driver is registered
// for DATABASE_DRIVER (blank-import its sqln/driver/<name> package to register
// it), so adding a new database requires no change here. The driver defaults to
// postgres when DATABASE_DRIVER is empty. Returns an error when the requested
// driver is not registered.
func Database(env *environment.Environment) (connection.Config, error) {
	name := connection.DriverName(strings.ToLower(strings.TrimSpace(env.DatabaseDriver)))
	if name == "" {
		name = connection.DriverPostgres
	}

	driver, ok := connection.GetDriver(name)
	if !ok {
		return connection.Config{}, fmt.Errorf(
			"config: database driver %q is not registered — blank-import its sqln/driver/%s package",
			name, name,
		)
	}

	pool := connection.DefaultPoolConfig()
	if env.DatabaseMaxOpenConns > 0 {
		pool.MaxOpenConns = env.DatabaseMaxOpenConns
	}
	if env.DatabaseMaxIdleConns > 0 {
		pool.MaxIdleConns = env.DatabaseMaxIdleConns
	}
	if env.DatabaseMaxLifetime > 0 {
		pool.MaxConnLifeTime = env.DatabaseMaxLifetime
	}

	return connection.Config{
		Driver: name,
		DSN: driver.DSN(connection.Settings{
			Host:     env.DatabaseHost,
			Port:     env.DatabasePort,
			User:     env.DatabaseUser,
			Password: env.DatabasePassword,
			Name:     env.DatabaseName,
			SSLMode:  env.DatabaseSSLMode,
		}),
		Pool: pool,
	}, nil
}

package database

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
)

// ConfigFromEnv builds a connection.Config from the DATABASE_* environment variables.
//
// It is driver-agnostic: the DSN is assembled by whichever driver is registered
// for DATABASE_DRIVER (blank-import its sqln/driver/<name> package to register
// it), so adding a new database requires no change here. The driver defaults to
// postgres when DATABASE_DRIVER is empty. Returns an error when the requested
// driver is not registered or the settings are invalid (connection.BuildDSN).
func ConfigFromEnv(env *environment.Environment) (connection.Config, error) {
	name := driverName(env)
	driver, ok := connection.GetDriver(name)
	if !ok {
		return connection.Config{}, fmt.Errorf(
			"database driver %q is not registered — blank-import its sqln/driver/%s package",
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
	pool.MaxIdleTime = env.DatabaseMaxIdleTime

	primary, replica := settingsFromEnv(env)
	dsn, err := connection.BuildDSN(driver, primary)
	if err != nil {
		return connection.Config{}, fmt.Errorf("DATABASE_*: %w", err)
	}
	policy, err := fullTextPolicy(env)
	if err != nil {
		return connection.Config{}, err
	}
	cfg := connection.Config{
		Driver: name, DSN: dsn, Pool: pool, QueryTimeout: env.DatabaseQueryTimeout,
		FullText: connection.FullTextConfig{Policy: policy},
	}
	if replica != nil {
		if cfg.ReadDSN, err = connection.BuildDSN(driver, *replica); err != nil {
			return connection.Config{}, fmt.Errorf("DATABASE_READ_*: %w", err)
		}
	}
	return cfg, nil
}

// fullTextPolicy reads DATABASE_FULLTEXT_POLICY. ensure creates missing
// indexes outside migrations, so it is refused in stage and prod.
func fullTextPolicy(env *environment.Environment) (fts.Policy, error) {
	switch p := strings.ToLower(strings.TrimSpace(env.DatabaseFullTextPolicy)); p {
	case "", "verify":
		return fts.Verify, nil
	case "trust":
		return fts.Trust, nil
	case "ensure":
		if t := env.GetEnvironmentType(); t == environment.ENV_PROD || t == environment.ENV_STAGE {
			return 0, fmt.Errorf("DATABASE_FULLTEXT_POLICY=ensure is not allowed in %s: create full-text indexes through migrations", t)
		}
		return fts.Ensure, nil
	default:
		return 0, fmt.Errorf("DATABASE_FULLTEXT_POLICY=%q, want verify, ensure or trust", p)
	}
}

// driverName is DATABASE_DRIVER, postgres when empty.
func driverName(env *environment.Environment) connection.DriverName {
	name := connection.DriverName(strings.ToLower(strings.TrimSpace(env.DatabaseDriver)))
	return cmp.Or(name, connection.DriverPostgres)
}

// settingsFromEnv returns the primary settings and, with DATABASE_READ_HOST,
// the replica's (nil otherwise).
func settingsFromEnv(env *environment.Environment) (connection.Settings, *connection.Settings) {
	primary := connection.Settings{
		Host:             env.DatabaseHost,
		Port:             env.DatabasePort,
		User:             env.DatabaseUser,
		Password:         env.DatabasePassword,
		Name:             env.DatabaseName,
		SSLMode:          env.DatabaseSSLMode,
		SSLRootCert:      env.DatabaseSSLRootCert,
		SSLCert:          env.DatabaseSSLCert,
		SSLKey:           env.DatabaseSSLKey,
		StatementTimeout: env.DatabaseStatementTimeout,
	}
	if env.DatabaseReadHost == "" {
		return primary, nil
	}
	replica := primary
	replica.Host = env.DatabaseReadHost
	replica.Port = cmp.Or(env.DatabaseReadPort, env.DatabasePort)
	return primary, &replica
}

// insecureSettings reports the primary and replica settings that would run in
// plaintext or without verifying the server, unless the host is local.
func insecureSettings(env *environment.Environment) []connection.Settings {
	primary, replica := settingsFromEnv(env)
	var out []connection.Settings
	for _, s := range []*connection.Settings{&primary, replica} {
		if s != nil && s.Insecure() && !s.IsLocal() {
			out = append(out, *s)
		}
	}
	return out
}

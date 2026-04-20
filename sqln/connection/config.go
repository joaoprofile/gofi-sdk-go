package connection

import "github.com/joaoprofile/gofi/sqln/migrate"

type Config struct {
	Driver DriverName
	DSN    string
	Pool   PoolConfig
}

// options
type Option func(*options)

type options struct {
	migrationConfig *migrate.Config
}

func WithMigrations(cfg migrate.Config) Option {
	return func(o *options) {
		o.migrationConfig = &cfg
	}
}

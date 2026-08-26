package connection

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/joaoprofile/gofi/sqln/driver"
	"github.com/joaoprofile/gofi/sqln/migrate"
)

const (
	ErrDriverNotRegistered = "database driver not registered"
	ErrPingFailed          = "database ping failed"
)

type Connection struct {
	db     *sql.DB
	driver Driver
}

func (c *Connection) DB() *sql.DB {
	return c.db
}

func (c *Connection) Close() error {
	return c.db.Close()
}

func (c *Connection) Dialect() driver.Dialect {
	return c.driver.Dialect()
}

// NewRaw builds a Connection from an existing *sql.DB and a Dialect.
// Useful when integrating with legacy code that manages the pool itself.
func NewRaw(db *sql.DB, d Driver) *Connection {
	return &Connection{db: db, driver: d}
}

func NewConnection(cfg Config, opts ...Option) (*Connection, error) {
	var opt options

	for _, o := range opts {
		o(&opt)
	}

	driver, ok := getDriver(cfg.Driver)
	if !ok {
		return nil, fmt.Errorf("%s: %s", ErrDriverNotRegistered, cfg.Driver)
	}

	db, err := driver.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open connection failed: %w", err)
	}

	applyPool(db, cfg.Pool)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", ErrPingFailed, err)
	}

	if opt.migrationConfig != nil {

		if err := migrate.Run(
			db,
			string(cfg.Driver),
			*opt.migrationConfig,
		); err != nil {
			return nil, fmt.Errorf("migration bootstrap failed: %w", err)
		}
	}

	startPoolMonitor(db, 30*time.Second)

	return &Connection{
		db:     db,
		driver: driver,
	}, nil
}

package connection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/driver"
	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
)

const (
	ErrDriverNotRegistered = "database driver not registered"
	ErrPingFailed          = "database ping failed"
)

type Connection struct {
	db          *sql.DB
	readDB      *sql.DB // nil without a replica
	driver      Driver
	stopMonitor func()
	id          string
	timeout     time.Duration // resolved Config.QueryTimeout; <= 0 disables it
	textPolicy  fts.Policy
	textReady   sync.Map // *fts.Index → struct{}: passed textPolicy
}

// QueryTimeout is the bound sqln applies to queries whose context has no
// deadline (see Config.QueryTimeout); a nil Connection has the default.
func (c *Connection) QueryTimeout() time.Duration {
	if c == nil {
		return DefaultQueryTimeout
	}
	return c.timeout
}

// WithQueryTimeout bounds ctx by QueryTimeout (see WithTimeout).
func (c *Connection) WithQueryTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return WithTimeout(ctx, c.QueryTimeout())
}

// ID identifies the database this connection reaches, so results cached for
// one database are never served to another. It is a hash of the driver and
// DSN (stable across processes sharing a cache); NewRaw gets a random one.
func (c *Connection) ID() string {
	return c.id
}

// dsnID hashes the DSN so the password it may carry never leaves memory in clear.
func dsnID(d DriverName, dsn string) string {
	sum := sha256.Sum256([]byte(string(d) + "\x00" + dsn))
	return "dsn:" + hex.EncodeToString(sum[:16])
}

// DB returns the primary pool.
func (c *Connection) DB() *sql.DB {
	return c.db
}

// ReadDB returns the replica pool, or the primary when none is configured.
func (c *Connection) ReadDB() *sql.DB {
	if c.readDB != nil {
		return c.readDB
	}
	return c.db
}

func (c *Connection) Close() error {
	if c.stopMonitor != nil {
		c.stopMonitor()
	}
	err := c.db.Close()
	if c.readDB != nil {
		err = errors.Join(err, c.readDB.Close())
	}
	return err
}

func (c *Connection) Dialect() driver.Dialect {
	return c.driver.Dialect()
}

// NewRaw builds a Connection from an existing *sql.DB and a Dialect.
// Useful when integrating with legacy code that manages the pool itself.
// Its ID is random: the pool's database is unknown, so cached results are not
// shared with other connections or processes.
func NewRaw(db *sql.DB, d Driver, opts ...Option) *Connection {
	var opt options
	for _, o := range opts {
		o(&opt)
	}
	c := &Connection{db: db, driver: d, id: "raw:" + rand.Text(), timeout: DefaultQueryTimeout}
	if opt.textPolicy != nil {
		c.textPolicy = *opt.textPolicy
	}
	return c
}

// RequireTextIndexes applies the connection's full-text policy (see
// FullTextConfig) to each index not yet accepted: with fts.Verify it fails
// with fts.ErrIndexMissing, fts.ErrIndexOutdated or fts.ErrServerConfig when
// the index is not in the database as defined. An accepted index is not
// checked again on this connection; a failure is, so applying the migration
// fixes it without a restart.
func (c *Connection) RequireTextIndexes(ctx context.Context, indexes ...*fts.Index) error {
	if c.textPolicy == fts.Trust {
		return nil
	}
	var pending []*fts.Index
	for _, idx := range indexes {
		if _, ok := c.textReady.Load(idx); !ok {
			pending = append(pending, idx)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	ctx, cancel := c.WithQueryTimeout(ctx)
	defer cancel()
	var errs []error
	for _, idx := range pending {
		if err := fts.Apply(ctx, c.textPolicy, c.db, c.Dialect(), idx); err != nil {
			errs = append(errs, err)
			continue
		}
		c.textReady.Store(idx, struct{}{})
	}
	return errors.Join(errs...)
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

	db, err := open(driver, cfg)
	if err != nil {
		return nil, err
	}

	var readDB *sql.DB
	if cfg.ReadDSN != "" {
		readCfg := cfg
		readCfg.DSN = cfg.ReadDSN
		if readDB, err = open(driver, readCfg); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("replica: %w", err)
		}
	}

	if opt.migrationConfig != nil {
		// A dedicated pool: golang-migrate pins one connection until closed, and
		// closing it also closes the *sql.DB it was given.
		migrationDB, err := driver.Open(cfg)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migration bootstrap failed: %w", err)
		}
		if err := migrate.RunAndClose(migrationDB, string(cfg.Driver), *opt.migrationConfig); err != nil {
			_ = db.Close()
			if readDB != nil {
				_ = readDB.Close()
			}
			return nil, fmt.Errorf("migration bootstrap failed: %w", err)
		}
	}

	conn := &Connection{
		db:          db,
		readDB:      readDB,
		driver:      driver,
		stopMonitor: startPoolMonitor(db, 30*time.Second),
		id:          dsnID(cfg.Driver, cfg.DSN),
		timeout:     resolveQueryTimeout(cfg.QueryTimeout),
		textPolicy:  cfg.FullText.Policy,
	}
	if len(cfg.FullText.Indexes) > 0 {
		if err := conn.RequireTextIndexes(context.Background(), cfg.FullText.Indexes...); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("full-text indexes: %w", err)
		}
	}
	return conn, nil
}

// open opens and pings a pool for cfg.
func open(d Driver, cfg Config) (*sql.DB, error) {
	db, err := d.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open connection failed: %w", err)
	}
	applyPool(db, cfg.Pool)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", ErrPingFailed, err)
	}
	return db, nil
}

// Package database is the gofi component for the SQL database (sqln).
//
// It links no SQL driver: blank-import the driver package for DATABASE_DRIVER
// (postgres by default) in main, or Build fails naming the missing import:
//
//	import _ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/postgres"
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/gofi"
	"github.com/joaoprofile/gofi-sdk-go/gofi/component/cache"
	"github.com/joaoprofile/gofi-sdk-go/gofi/config/core"
	"github.com/joaoprofile/gofi-sdk-go/obs/metrics"
	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
)

// migrationsPath is where DATABASE_MIGRATION=true reads the migrations from.
const migrationsPath = ".migrations"

// Component opens the database and publishes it as the global sqln connection.
type Component struct {
	db       *sql.DB
	replica  *sql.DB // set when DATABASE_READ_HOST is configured
	injected bool
}

// New opens the database from DATABASE_* (see ConfigFromEnv) during Build.
// With DATABASE_MIGRATION=true it runs the migrations in .migrations first.
func New() *Component { return &Component{} }

// FromDB uses a *sql.DB opened by the caller, who owns and closes it. It gets
// the same health check and pool metrics, but is not made the global sqln
// connection.
func FromDB(db *sql.DB) *Component {
	return &Component{db: db, injected: true}
}

func (c *Component) Name() string      { return "database" }
func (c *Component) Stage() gofi.Stage { return gofi.StageDatabase }

func (c *Component) Start(_ context.Context, rt *gofi.Runtime) error {
	// sqln's query cache reads CACHE_* lazily; configure it for queries that use it.
	cache.Configure(rt.Env())

	if !c.injected {
		if err := c.open(rt); err != nil {
			return err
		}
	}
	if c.db == nil {
		return nil
	}

	if err := metrics.ObserveDBStats("main", c.db); err != nil {
		return fmt.Errorf("pool metrics: %w", err)
	}
	if err := metrics.ObserveDBStats("replica", c.replica); err != nil {
		return fmt.Errorf("pool metrics: %w", err)
	}
	rt.AddHealthCheck("database", c.db.PingContext)
	if c.replica != nil {
		rt.AddHealthCheck("database-replica", c.replica.PingContext)
	}
	return nil
}

// InsecureTransports reports a primary or replica on a non-local host whose
// DATABASE_SSL_MODE does not verify the server. Injected pools are not checked.
func (c *Component) InsecureTransports(env *environment.Environment) []gofi.InsecureTransport {
	if c.injected {
		return nil
	}
	var out []gofi.InsecureTransport
	for _, s := range insecureSettings(env) {
		out = append(out, gofi.InsecureTransport{
			Resource: core.ResourceDatabase,
			Setting:  "DATABASE_SSL_MODE",
			Detail:   fmt.Sprintf("host %s uses sslmode %s", s.Host, s.EffectiveSSLMode()),
		})
	}
	return out
}

func (c *Component) open(rt *gofi.Runtime) error {
	env := rt.Env()
	cfg, err := ConfigFromEnv(env)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	var opts []connection.Option
	if env.DatabaseMigration {
		opts = append(opts, connection.WithMigrations(migrate.Config{Path: migrationsPath}))
	}
	conn, err := connection.NewConnection(cfg, opts...)
	if err != nil {
		return fmt.Errorf("%s: %w", cfg.Driver, err)
	}
	connection.SetGlobal(conn)
	c.db = conn.DB()
	if conn.ReadDB() != conn.DB() {
		c.replica = conn.ReadDB()
	}
	rt.OnClose(func(context.Context) error { return conn.Close() })
	return nil
}

// DB returns the primary pool; nil before Build.
func (c *Component) DB() *sql.DB { return c.db }

// ReadDB returns the read replica pool, or the primary when there is none.
func (c *Component) ReadDB() *sql.DB {
	if c.replica != nil {
		return c.replica
	}
	return c.db
}

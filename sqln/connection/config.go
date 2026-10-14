package connection

import (
	"context"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/sqln/fts"
	"github.com/joaoprofile/gofi-sdk-go/sqln/migrate"
)

type Config struct {
	Driver DriverName
	DSN    string
	// ReadDSN points at a read replica. When set, queries run by the sqln
	// query manager outside a transaction use it; writes, statements and
	// transactions always use DSN. Expect replica lag on reads.
	ReadDSN string
	Pool    PoolConfig
	// Password, when set, is called for every new physical connection and
	// overrides the DSN password: short-lived IAM tokens (RDS, Aurora) are
	// fetched as the pool grows. Supported by the postgres driver, which
	// requires sslmode verify-ca or verify-full with it.
	Password func(ctx context.Context) (string, error)
	// QueryTimeout bounds each query run by sqln whose context has no
	// deadline. Zero uses DefaultQueryTimeout; negative disables it.
	QueryTimeout time.Duration
	// FullText decides how searches on full-text indexes are checked.
	FullText FullTextConfig
}

// FullTextConfig applies an fts.Policy to the full-text indexes searched
// through a connection.
type FullTextConfig struct {
	// Policy defaults to fts.Verify: each index is checked in the catalog the
	// first time it is searched, and the search fails when it is missing or
	// was built from another definition.
	Policy fts.Policy
	// Indexes are checked (or created, with fts.Ensure) when the connection
	// opens, after migrations, so a missing index fails the startup instead
	// of the first search.
	Indexes []*fts.Index
}

// DefaultQueryTimeout bounds queries when Config.QueryTimeout is zero.
const DefaultQueryTimeout = 30 * time.Second

// WithTimeout returns ctx bounded by d, unless ctx already has a deadline or
// d is not positive. Always call cancel.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok || d <= 0 {
		return ctx, func() {
			// Nothing to release: no derived context was created.
		}
	}
	return context.WithTimeout(ctx, d)
}

func resolveQueryTimeout(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultQueryTimeout
	}
	return d
}

// options
type Option func(*options)

type options struct {
	migrationConfig *migrate.Config
	textPolicy      *fts.Policy
}

// WithFullTextPolicy sets the full-text policy of a connection built by
// NewRaw (Config.FullText does it for NewConnection); use fts.Trust with a
// mocked database.
func WithFullTextPolicy(p fts.Policy) Option {
	return func(o *options) {
		o.textPolicy = &p
	}
}

func WithMigrations(cfg migrate.Config) Option {
	return func(o *options) {
		o.migrationConfig = &cfg
	}
}

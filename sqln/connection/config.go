package connection

import (
	"context"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/sqln/migrate"
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
}

func WithMigrations(cfg migrate.Config) Option {
	return func(o *options) {
		o.migrationConfig = &cfg
	}
}
